O DWYT é um orquestrador local para reduzir consumo de tokens em ferramentas de IA. Ele roda como um binário Go único, serve uma UI React em http://127.0.0.1:2737, mantém estado em ~/.dwyt/, e integra quatro frentes: Obsidian como memória obrigatória do projeto, RTK para comprimir saídas de terminal, Headroom como proxy de compressão de APIs, e Codebase Memory MCP para mapa estrutural do código.

A arquitetura principal está em server.go (line 185), a tela principal em Dashboard.tsx (line 33), o setup em SetupWizard.tsx (line 10), e a geração de arquivos de integração em integrate.go (line 16).

O Que Ele Faz

dwyt . inicia ou reaproveita um daemon local e abre o dashboard.
O backend Gin expõe APIs em /api/* para status, setup, logs, projetos, Obsidian, Headroom, Codebase e MCP registry.
A UI tem um Setup Wizard para escolher ferramentas e clientes de IA: Claude, Codex, Copilot, Kiro, Cursor e OpenCode.
O setup gera arquivos como AGENTS.md, CLAUDE.md, .mcp.json, .cursor/rules/dwyt.mdc, .kiro/steering/dwyt.md e opencode.json.
O dashboard mostra cards de Codebase, RTK, Headroom e Obsidian, além de métricas globais de economia.
O Obsidian deveria criar um vault por projeto em ~/.dwyt/projects/<id>/obsidian/, com busca, save, resumo e abertura do vault.
O Codebase indexa o repositório sob demanda e deveria expor/abrir a UI do grafo na porta 9749.
O RTK coleta métricas de comandos e tokens economizados.
O Headroom deveria iniciar proxy na porta 8787 e mostrar estatísticas.
O registry MCP deveria configurar servidores MCP para os clientes suportados.

Validação Executada


go test ./...: passou, 17 testes em 22 pacotes.

npm run build: passou.

npm run lint: falhou com 100 erros e 7 warnings.

Screenshot desktop: renderiza, mas mostra cards como Not Installed mesmo com endpoints indicando instalações.

Screenshot mobile 390x844: layout quebra horizontalmente; o grid continua em duas colunas e corta cards/botões.

/api/health: OK.

/api/status: responde ferramentas, mas conflita com outros endpoints.

/api/obsidian/status: retorna active:false, no Obsidian vault loaded.

/api/mcp/registry: mostra dwyt-codebase instalado/offline e dwyt-obsidian não instalado.

.mcp.json: contém apenas um servidor chamado dwyt, apontando para codebase, sem Obsidian.


Achados Críticos


MCP obrigatório não está correto.

O requisito pede MCPs obrigatórios e nomeados para codebase e obsidian, mas o projeto atual gera nomes diferentes e incompletos. O registry usa dwyt-codebase e dwyt-obsidian em registry.go (line 40), o dashboard procura codebase e obsidian-mcp, e .mcp.json usa apenas dwyt. Além disso, o Setup seleciona obsidian, mas só instala obsidian-mcp se vier explicitamente esse ID em server.go (line 643).



Dashboard mostra informações inconsistentes.

/api/status diz que Obsidian está ativo, mas /api/obsidian/status diz que não há vault carregado. /api/status indica Codebase rodando, mas /api/services/codebase/status diz running:false. Isso compromete a confiança no dashboard.



Botões têm problemas funcionais.

No card RTK, Start e Stop chamam startAll() e stopAll(), embora RTK não seja daemon. Isso pode iniciar/parar Codebase e Headroom a partir do card errado. Os botões Configure MCP de Codebase e Obsidian chamam a mesma ação genérica, sem indicar qual MCP será configurado.



Botões precisam de melhoria estética e UX.

Há mistura de CSS global, Tailwind inline e botões inline com gradientes. Os botões são pequenos, inconsistentes, sem ícones, sem tooltip, sem foco acessível claro e com estados de loading frágeis. O botão Open Graph manipula document.activeElement.textContent diretamente, o que é frágil.



Obsidian, regra central do produto, está quebrado no estado atual.

A UI diz “Obsidian vault active”, mas a API dedicada retorna que não há vault. Save/Search/Rebuild/Open existem, mas não têm como funcionar corretamente se ProjectObsidian estiver nil.



Dados do dashboard não refletem a realidade do grafo.

O codebase-memory MCP indexado nesta sessão tem 904 nodes e 2330 edges, mas /api/projects/current mostra nodes:0 e edges:0. O endpoint de indexação marca MarkIndexed(path, 0, 0) em server.go (line 362), então perde as métricas reais.



E2E está desatualizado.

test-e2e.sh (line 1) testa /api/brain/*, mas o servidor atual registra /api/obsidian/*. Não rodei esse teste porque ele mata daemons dwyt via pkill e está incompatível com as rotas atuais.



Relatório De Melhorias

Prioridade máxima:

Padronizar MCPs para nomes obrigatórios codebase e obsidian em registry, dashboard, .mcp.json, .vscode/mcp.json, .claude/mcp.json, .kiro/mcp.json e OpenCode.
Fazer o Setup instalar sempre o MCP do Obsidian quando Obsidian for obrigatório.
Corrigir o dashboard para ler as mesmas chaves que o registry retorna, ou melhor, criar um contrato único de status.
Unificar /api/status, /api/tool-details, /api/services/*/status, /api/obsidian/status e /api/mcp/registry para não exibirem estados contraditórios.
Corrigir Obsidian para carregar o vault do projeto ativo no daemon e em troca de projeto.
Fazer indexação do Codebase persistir nodes/edges reais, não 0,0.

Prioridade alta:

Trocar Start/Stop do card RTK por ação informativa, porque RTK é CLI.
Dar botões próprios para Codebase Start/Stop, se o produto promete isso no README.
Separar Configure MCP codebase e Configure MCP obsidian, com feedback claro.
Adicionar testes de API para todos os botões do dashboard.
Atualizar ou remover o E2E antigo de /api/brain/*.
Resolver o lint do frontend antes de tratar a UI como estável.

Prioridade estética:

Criar um componente único Button com variantes primary, secondary, success, danger, ghost e icon.
Substituir textos pequenos por ícones onde fizer sentido: refresh, logs, setup, start, stop, open, search.
Adicionar tooltip, foco por teclado, estado loading e disabled consistente.
Remover gradientes fortes dos botões pequenos; usar cor sólida ou fundo sutil.
Fazer o dashboard mobile virar uma coluna única abaixo de 768px.
Evitar texto cortado em botões e cards; hoje o mobile corta a tela horizontalmente.

Resumo Final

O projeto tem uma ideia forte e uma base técnica boa: Go simples, React enxuto, daemon local, APIs claras e integração com ferramentas úteis. Mas, no estado atual, as regras centrais ainda não estão confiáveis: Obsidian aparece ativo e inativo ao mesmo tempo, MCP obrigatório não é criado/nomeado como solicitado, o dashboard mostra estados conflitantes, e alguns botões executam ações erradas ou genéricas demais. O próximo melhor passo é corrigir primeiro o contrato de status + MCPs obrigatórios; depois vem a lapidação estética dos botões e a bateria E2E real.