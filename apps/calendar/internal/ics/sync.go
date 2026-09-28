package ics

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/aquasp/kuracalendar/internal/store"
)

// Getter downloads a feed body. Nil means Fetch.
type Getter func(ctx context.Context, rawURL string) ([]byte, error)

var syncGate sync.Mutex

// SyncFeed fetches one feed and replaces its events. A fetch or parse
// failure keeps the previous events and records a short error code.
// Database failures are returned. The feed URL is never logged.
func SyncFeed(ctx context.Context, st *store.Store, feed *store.ICSFeed, get Getter, now time.Time, loc *time.Location) error {
	if feed == nil {
		return store.ErrICSNotFound
	}
	syncGate.Lock()
	defer syncGate.Unlock()
	if get == nil {
		get = Fetch
	}
	if loc == nil {
		loc = time.UTC
	}
	// Window follows the user's civil date, not the process zone.
	now = now.In(loc)
	body, err := get(ctx, feed.URL)
	if err != nil {
		code := feedErrorCode(err)
		log.Printf("ics feed %d: %s", feed.ID, code)
		return st.MarkICSFeedError(feed.UserID, feed.ID, code)
	}
	from, to := Window(now)
	events, err := Parse(body, loc, from, to)
	if err != nil {
		log.Printf("ics feed %d: parse", feed.ID)
		return st.MarkICSFeedError(feed.UserID, feed.ID, "parse")
	}
	inputs := make([]store.ICSEventInput, 0, len(events))
	for _, e := range events {
		inputs = append(inputs, store.ICSEventInput{
			UID: e.UID, Title: e.Title, Body: e.Body, AllDay: e.AllDay,
			StartsOn: e.StartsOn, EndsOn: e.EndsOn, StartsAt: e.StartsAt, EndsAt: e.EndsAt,
		})
	}
	_, _, err = st.ReplaceICSEvents(feed.UserID, feed.ID, inputs)
	return err
}

func feedErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrBlocked):
		return "blocked"
	case errors.Is(err, ErrLarge):
		return "large"
	case errors.Is(err, ErrParse):
		return "parse"
	default:
		return "fetch"
	}
}

// RunPeriodic refreshes every unpaused feed on a timer. It also runs once
// at start. Failures are logged by feed id only.
func RunPeriodic(st *store.Store, every time.Duration) {
	if every < time.Minute {
		every = 15 * time.Minute
	}
	go func() {
		syncActive(st)
		t := time.NewTicker(every)
		defer t.Stop()
		for range t.C {
			syncActive(st)
		}
	}()
}

func syncActive(st *store.Store) {
	feeds, err := st.ListActiveICSFeeds()
	if err != nil {
		log.Printf("ics sync list failed")
		return
	}
	zones := map[int64]*time.Location{}
	for _, f := range feeds {
		loc, ok := zones[f.UserID]
		if !ok {
			loc = time.UTC
			if u, err := st.FindUser(f.UserID); err == nil {
				loc = u.Zone()
			}
			zones[f.UserID] = loc
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		if err := SyncFeed(ctx, st, f, nil, time.Now(), loc); err != nil {
			log.Printf("ics feed %d: store", f.ID)
		}
		cancel()
	}
}

// SyncUserFeeds rewrites one user's unpaused feeds in loc. A failed fetch
// leaves that feed's previous rows in place.
func SyncUserFeeds(ctx context.Context, st *store.Store, userID int64, get Getter, now time.Time, loc *time.Location) error {
	feeds, err := st.ListICSFeeds(userID)
	if err != nil {
		return err
	}
	for _, f := range feeds {
		if f.Paused {
			continue
		}
		if err := SyncFeed(ctx, st, f, get, now, loc); err != nil {
			log.Printf("ics feed %d: store", f.ID)
		}
	}
	return nil
}
