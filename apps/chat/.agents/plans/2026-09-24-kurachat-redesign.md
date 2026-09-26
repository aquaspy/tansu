## Goal

Redesenhar o KuraChat da branch `go` com uma identidade nova, moderna e minimalista — nova paleta de cores, nova hierarquia visual e acabamento de alto padrão em desktop e mobile — sem remover nem alterar nenhuma feature existente.

## Success Criteria

- Todas as telas (login, signup, unlock, troca de senha, shell com lista + conversa, página compartilhada `/s/`, 404/500) usam o novo sistema visual em light, dark e system.
- Todas as features atuais continuam funcionando: streaming SSE, busca web/deep, anexos, pickers de modelo/effort, custo, share links, retry, busca de conversas, lock/auto-lock, offline, PWA, i18n en/pt.
- Layout desktop com coluna de leitura centralizada e composer em dock flutuante; layout mobile com composer ancorado, menu em bottom-sheet e alvos de toque ≥ 44px.
- Contraste de texto AA (4.5:1 corpo, 3:1 texto grande/muted decorativo justificado) nos dois temas.
- CI verde: `templ generate` limpo, `gofmt`, `go vet`, `go test`, build do CSS e build do binário estático.

## Context And Current Facts

- Branch atual: `go` (árvore com o rewrite já presente: `cmd/`, `internal/`, `web/`). Stack: Go + `templ` + `htmx` + SSE puro (sem websocket) + JS vanilla (`web/static/js/app.js`, ~724 linhas, controllers via `data-controller`/`data-action`) + Tailwind v4 (standalone compila `web/static/css/input.css` → `web/static/css/app.css`). Dev via `bin/dev` (watchers); Docker e CI reproduzem `templ generate` + build do CSS + binário estático.
- Visual atual: paleta terrosa quente (`--paper #f7f3eb`, `--accent #b55220` no light; `#1a1916`/`#d46a32` no dark), wordmark e superfícies em serifada, grid de 2 colunas com sidebar de 16rem, mensagens em bolhas (`msg-user` com `highlight`, `msg-assistant` em card com borda).
- Templates em `internal/views/*.templ` (+ `*_templ.go` gerados): `layout`, `shell` (sidebar + coluna), `thread` (barra, settings, transcript, composer), `message` (artigos, streaming, retry, meta, tools, citações), `list`, `fragments` (título, custo), `share` (botão + painel), `auth` (4 páginas), `sharedpage`, `shared` (wordmark, theme-switch, sign-out).
- Contratos que o redesign não pode quebrar: ids de DOM (`message_*`, `body_message_*`, `status_message_*`, `title_conversation_*`, `title_field_conversation_*`, `cost_conversation_*`, `share-panel-*`, `share-btn-*`, `#transcript`, `#conversation-list`, `#msg-echo`); eventos SSE (`status/body/message/title/title-field/cost-*`); atributos `hx-*` e swaps OOB; nomes de `data-controller`/`data-action`/`data-*-target` consumidos pelo `app.js`; hooks de classe lidos pelo JS (`.msg`, `.msg-user`, `.msg-body`, `.msg-image`, `.composer-chip`, `.mobile-menu`, `form.composer`, `[data-theme-button]`, `[data-theme-choice]`); lógica de viewport mobile (`--vvh`/`--vvt`); meta `theme-color`; nomes de cache do service worker.
- Features a preservar (inventário fechado): auth (login, signup com chave, unlock, troca de senha, logout com limpeza do cache offline); lista (nova conversa, busca com debounce via htmx, excluir uma/todas com confirmação, título ao vivo via SSE); editor (renomear com autosave, custo ao vivo, share criar/girar/revogar + copiar URL, excluir); settings (picker de modelo quando ≥ 2, picker de effort, persistência sticky); transcript (streaming com linha de status, falha + retry, meta de modelo/busca, copiar, fold de fontes); composer (autoresize, envio com estados desabilitado, até 4 imagens via picker + paste + chips removíveis, toggles web/deep sticky com regra deep→web, eco otimista, Cmd/Ctrl+Enter); shell (tema 3 estados, auto-lock 15min + 2min background, banner offline, FAB mobile, flashes, diálogos de confirmação); página `/s/` somente-leitura com `noindex`; PWA (manifest, SW, ícones).

## Constraints And Non-goals

- Sem novas dependências externas (sem webfonts remotas, sem framework CSS/JS novo): tudo em CSS próprio + tokens, preservando o PWA offline.
- Sem mudança de rotas, API, schema SQLite, nomes de eventos SSE, ids de DOM ou chaves i18n.
- Sem alteração de comportamento de negócio (custos, compactação, retenção, ZDR): só apresentação.
- Não inclui: redraw do ícone do app além de recolorir para a nova paleta (ver Open Questions), modo offline de escrita, novos idiomas, testes visuais automatizados.

## Key Decisions

- **Paleta nova "Abyss + Lagoon" (dark-first, teal como acento único).** Rejeitado manter a família terracota (o pedido exige paleta nova) e rejeitado gradientes coloridos/roxo-neon (conflitam com "minimalista" e com leitura longa de chat). Tokens:
  - Dark: app `#0A0D12`, sidebar `#0D1117`, superfície `#131926`, input `#0F141D`, borda `#222B3A`, texto `#E9EEF5`, muted `#8B95A7`, acento `#2DD4BF` (texto sobre acento `#062A26`), links `#5EEAD4`, perigo `#F97066`.
  - Light: app `#F7F8F7`, sidebar `#EFF1EE`, superfície `#FFFFFF`, input `#FFFFFF`, borda `#E2E7E2`, texto `#0F1512`, muted `#667085`, acento `#0E7490` (texto sobre acento `#FFFFFF`), perigo `#D92D20`.
  - Estados derivados com `color-mix` (já usado no codebase): seleção ativa = acento 12–14%, hover = acento 8%.
- **Tipografia 100% system sans; serifada sai.** Mantém zero dependências e cara moderna; mono system (`ui-monospace`) só em código. Rejeitado importar Inter/Geist (quebraria offline-first e adicionaria build).
- **Assistente sem card: texto corrido full-width; usuário em bolha compacta à direita.** É o padrão moderno minimalista (menos cromo, mais leitura) e reduz a altura do transcript. Meta + toolbar do assistente viram linha sutil abaixo do texto.
- **Settings somem como barra: modelo/effort viram controles compactos no rodapé do composer.** Economiza ~40px verticais e agrupa "controles do turno" (modelo, effort, web, deep, anexo) num lugar só. O form `hx-patch` + CSRF se move junto, sem mudar semântica. Fallback se ficar apertado no mobile: esforço vira segmented control de 3–4 opções e modelo vira select nativo estilizado.
- **Composer vira dock flutuante** (rounded-2xl, borda, sombra, largura máxima da coluna de leitura) com fileira de toolbar; no mobile ancora acima da safe-area. Mantém `form.composer`, targets, chips e regras de disabled.
- **Menu mobile vira bottom-sheet** (dialog nativo, mesma mecânica dos confirms) em vez de dropdown absoluto; mantém itens e ações. FAB "nova conversa" vira pílula com rótulo.
- **Nomes de classes-hook do JS ficam estáveis** (lista em Contexto); o redesenho é CSS + reestruturação interna de markup. Qualquer classe lida pelo `app.js` que precisar mudar será alterada em par com o JS na mesma unidade de trabalho.
- **Cache do service worker:** verificar a estratégia do `sw.js` para `app.css` e versionar se necessário, senão usuários PWA verão CSS velho com markup novo.

## Recommended Approach

Reescrita visual em camadas, de baixo para cima: (1) fundação (tokens, tipografia, reset, botões/inputs/diálogos genéricos); (2) shell + sidebar; (3) transcript + mensagens; (4) composer + controles; (5) menus/diálogos/flashes; (6) auth/lock/share/estáticas + PWA; (7) polimento responsivo + acessibilidade + contraste. Cada camada edita `input.css` + os `.templ` da superfície e regenera `*_templ.go`, mantendo todos os contratos listados. Nenhuma camada muda Go de handler/chat/store/i18n exceto o `theme-color` dinâmico no `app.js` se a paleta exigir (cores hardcoded `#141311`/`#f4f0e8` hoje).

## Work Plan

1. **Fundação visual** — novos tokens light/dark/system em `web/static/css/input.css`, tipografia sans, escala de raio/sombra/espaçamento, estilos base de botões/inputs/selects/diálogos/focus-visible, `prefers-reduced-motion`. Arquivos: `input.css`, `layout.templ` (meta `theme-color` padrão), `app.js` (cores do `paintTheme`). Validação: páginas auth renderizam nos 3 temas sem flash.
2. **Shell + sidebar (desktop)** — grid, sidebar com busca estilo command, botão nova conversa, fileiras com hover + delete, rodapé reorganizado. Arquivos: `shell.templ`, `list.templ`, `shared.templ`, `input.css`. Validação: busca htmx, delete/confirm, título ao vivo via SSE.
3. **Transcript + mensagens** — assistente corrido, bolha do usuário, imagens, blocos de código/tabelas/citações, meta sutil, toolbar fantasma, estado streaming com caret/shimmer + linha de status, estado falha + retry. Arquivos: `message.templ`, `fragments.templ`, `input.css`. Validação: streaming fim-a-fim, retry, copiar, fold de fontes.
4. **Barra do editor + composer** — topbar slim (voltar, título autosave, custo, share, delete), settings dobrados para dentro do composer, dock flutuante com toolbar (anexo, web, deep, enviar). Arquivos: `thread.templ`, `input.css`, sem JS novo (regra deep→web intacta). Validação: autosave de título, toggles sticky, anexo/paste/chips, eco otimista, envio offline desabilitado.
5. **Menus, diálogos, flashes** — bottom-sheet mobile, confirms e share-dialog reestilizados, flashes toast. Arquivos: `shell.templ`, `share.templ`, `list.templ`, `input.css`, `app.js` (só se seletor mudar). Validação: abrir/fechar/backdrop, copiar link, criar/girar/revogar share, delete-all.
6. **Auth, lock, share pública, estáticas, PWA** — 4 páginas auth + `/s/` + `404/500.html` no novo sistema; `theme-color`, manifest, ícones recoloridos; auditoria + bump do `sw.js` para o CSS novo. Arquivos: `auth.templ`, `sharedpage.templ`, `web/static/*`, `sw.js`. Validação: fluxos login/signup/unlock/troca-senha/logout, página `/s/` em aba anônima, update do SW.
7. **Responsivo + acessibilidade + consistência** — breakpoints (desktop ≥861px, mobile), safe-area + `--vvh`, alvos ≥44px, foco visível, `aria` preservado, auditoria de contraste AA nos dois temas, `templ generate` final + `gofmt`. Validação: matriz manual (desktop/mobile × light/dark) + checklist de features do inventário.

## Validation Plan

- Comandos (espelham o CI): `templ generate && git diff --exit-code -- internal/views/`, `gofmt -l cmd internal` vazio, `go vet ./...`, `go test ./...`, build Tailwind standalone (minify) e `CGO_ENABLED=0 go build` do binário.
- Manual desktop (Chrome/Firefox, light + dark): login → nova conversa → enviar → streaming até o fim → toggles web/deep → anexo → copiar → fontes → renomear → custo atualiza → share cria/copia/revoga → busca → deletes → troca de senha → logout → unlock via lock.
- Manual mobile (viewport ≤860px + device real se possível): topbar, bottom-sheet, FAB pílula, composer com teclado aberto (sem pulo de layout), rotação, offline (banner + envio bloqueado + releitura de chat aberto).
- PWA: instalar, atualizar SW e confirmar CSS novo; `/s/` em sessão anônima; contraste amostrado (texto/muted/acento) nos dois temas.
- Passo de maior risco: streaming fim-a-fim após mexer em `message.templ`/`thread.templ` (SSE + swaps OOB + eco otimista) — validar primeiro em cada camada que tocar o transcript/composer.

## Risks / Rollback

- **Quebrar contrato SSE/htmx/JS** (telas param de atualizar): mitigado pela regra de classes estáveis + validação de streaming por camada; rollback = `git checkout` dos `.templ`/`.css` da camada.
- **CSS velho em PWA instalada** (markup novo + `app.css` cacheado): mitigado pelo bump do SW na unidade 6; testar update antes de considerar pronto.
- **Regressão de teclado mobile** (viewport/safe-area): mitigado por não tocar na lógica `--vvh`/`--vvt` e validar com teclado aberto; rollback isolado no bloco `@media`.
- **Árvore `go` já suja** (deletes staged + arquivos novos untracked): antes de implementar, conferir `git status` e sugerir snapshot/commit de segurança — sem isso o rollback por camada fica impreciso. Nenhum commit será feito sem pedido explícito.
- **Contraste do teal no light** (`#0E7490` em fundos claros): validar AA; se falhar, escurecer acento light para `#0C6A84` sem trocar a família.

## Open Questions

1. A família teal sobre "quase-preto" (dark-first) está aprovada como direção, ou prefere outra família (ex.: azul iris, verde)?
2. O redraw/recolor do ícone do app (`icon.svg`/`icon.png`/`apple-touch-icon.png`) e da wordmark entra neste redesign ou fica para depois?
