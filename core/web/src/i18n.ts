// ── DWYT i18n — EN (default) / PT-BR ─────────────────────────────────────────

export type Lang = 'en' | 'pt'

export const T = {
  en: {
    // Header
    auto: 'Auto', refresh: '↺ Refresh', logs: 'Logs', hideLogs: 'Hide Logs', setup: '← Setup',
    // Totals banner
    withoutDwyt: 'Without DWYT', withDwyt: 'With DWYT', totalSavings: 'Total Savings',
    wouldBeSpent: 'tokens would be spent', tokensSpent: 'tokens spent', tokensSaved: 'tokens saved',
    noDataTitle: 'Without DWYT you would spend far more tokens',
    noDataSub: 'Install the tools and start using them — savings data will appear here.',
    // Logs
    logsTitle: 'Logs',
    // Card labels
    tokensSavedLabel: 'Tokens saved', uptime: 'Active', repos: 'Repos',
    scope: 'Scope', port: 'Port', requests: 'Requests', compression: 'Compression',
    commands: 'Commands', savingsPct: '% savings', activeSince: 'Active',
    // Card status — 3 states
    notInstalled: 'Not Installed', inactive: 'Inactive', active: 'Active',
    // Card actions
    start: '▶ Start', stop: '■ Stop',
    index: 'Index', indexing: '...',
    openGraph: 'Open Graph →', openStats: 'Open Stats →',
    search: 'Search', searchPlaceholder: 'Search obsidian...', repoPlaceholder: 'path/to/repo',
    rebuildSummary: 'Rebuild summary', forgetMemory: 'Forget obsidian', forgetMemoryConfirm: 'Forget all obsidian data for this project?',
    saveMemory: 'Save to obsidian', saveMemoryPlaceholder: 'Obsidian note...',
    memories: 'Obsidian files', noMemoriesYet: 'No obsidian files yet',
    global: 'global',
    notIndexed: 'Not indexed', indexed: 'Indexed', reindex: 'Reindex', indexingInBg: 'Indexing in background...',
    openGraphUnavailable: 'Open Graph (may need index first) →',
    status: 'Status',
    allRepos: 'All repositories', selectProject: 'Select a project', repoCount: 'repositories',
    openDashboard: 'Open dashboard', openBrain: 'Open in Obsidian', viewLogs: 'View Logs',
    // User-friendly tool names
    brainActive: 'Obsidian', compressionActive: 'Headroom',
    terminalOptimized: 'RTK', codeMap: 'Codebase',
    // Setup
    setupTitle: 'DWYT Setup', install: 'Install →', installing: 'Installing...',
    dashboard: 'Dashboard →', loading: 'Loading...', starting: 'Starting...',
    tools: 'Tools', clients: 'AI Clients', project: 'Project',
    selected: 'selected', of: 'of', noneSelected: 'None selected',
    projectPlaceholder: 'Project path...', selectDir: 'Select this directory',
    goUp: '← Up',
    toolsInstalling: 'Installing tools in background. Please wait.',
    // Tool descriptions
    cbmcpDesc: 'Code graph — structural exploration',
    brainDesc: 'Obsidian vault — project knowledge base',
    headroomDesc: 'API call compression',
    rtkDesc: 'Terminal output compression',
    claudeDesc: 'CLAUDE.md + .claude/',
    codexDesc: 'AGENTS.md + .codex/',
    copilotDesc: '.github/copilot-instructions.md',
    kiroDesc: '.kiro/steering/dwyt.md',
    cursorDesc: '.cursor/rules/dwyt.mdc',
    opencodeDesc: 'opencode.json + AGENTS.md',
    variable: 'variable',
  },
  pt: {
    auto: 'Auto', refresh: '↺ Atualizar', logs: 'Logs', hideLogs: 'Esconder Logs', setup: '← Setup',
    withoutDwyt: 'Sem DWYT', withDwyt: 'Com DWYT', totalSavings: 'Economia Total',
    wouldBeSpent: 'tokens seriam gastos', tokensSpent: 'tokens gastos', tokensSaved: 'tokens economizados',
    noDataTitle: 'Sem DWYT você gastaria muito mais tokens',
    noDataSub: 'Instale as ferramentas e comece a usar — os dados de economia aparecerão aqui.',
    logsTitle: 'Logs',
    tokensSavedLabel: 'Tokens economizados', uptime: 'Ativo', repos: 'Repos',
    scope: 'Escopo', port: 'Porta', requests: 'Requisições', compression: 'Compressão',
    commands: 'Comandos', savingsPct: '% economia', activeSince: 'Ativo',
    // 3 estados
    notInstalled: 'Não instalado', inactive: 'Inativo', active: 'Ativo',
    start: '▶ Iniciar', stop: '■ Parar',
    index: 'Indexar', indexing: '...',
    openGraph: 'Abrir Grafo →', openStats: 'Abrir Stats →',
    search: 'Buscar', searchPlaceholder: 'Buscar no obsidian...', repoPlaceholder: 'path/to/repo',
    rebuildSummary: 'Reconstruir resumo', forgetMemory: 'Apagar obsidian', forgetMemoryConfirm: 'Apagar todos os dados do obsidian?',
    saveMemory: 'Salvar no obsidian', saveMemoryPlaceholder: 'Nota do obsidian...',
    memories: 'Arquivos do obsidian', noMemoriesYet: 'Nenhum arquivo ainda',
    global: 'global',
    notIndexed: 'Não indexado', indexed: 'Indexado', reindex: 'Reindexar', indexingInBg: 'Indexando em background...',
    openGraphUnavailable: 'Abrir Grafo (pode precisar indexar) →',
    status: 'Status',
    allRepos: 'Todos os repositórios', selectProject: 'Selecionar projeto', repoCount: 'repositórios',
    openDashboard: 'Abrir dashboard', openBrain: 'Abrir no Obsidian', viewLogs: 'Ver Logs',
    // User-friendly tool names
    brainActive: 'Obsidian', compressionActive: 'Headroom',
    terminalOptimized: 'RTK', codeMap: 'Codebase',
    setupTitle: 'DWYT Setup', install: 'Instalar →', installing: 'Instalando...',
    dashboard: 'Dashboard →', loading: 'Carregando...', starting: 'Iniciando...',
    tools: 'Ferramentas', clients: 'IAs / Clientes', project: 'Projeto',
    selected: 'selecionadas', of: 'de', noneSelected: 'Nenhum selecionado',
    projectPlaceholder: 'Caminho do projeto...', selectDir: 'Selecionar este diretório',
    goUp: '← Subir',
    toolsInstalling: 'Ferramentas sendo instaladas em background. Aguarde.',
    cbmcpDesc: 'Grafo de código — exploração estrutural',
    brainDesc: 'Vault Obsidian — base de conhecimento do projeto',
    headroomDesc: 'Compressão de chamadas à API',
    rtkDesc: 'Compressão de output de terminal',
    claudeDesc: 'CLAUDE.md + .claude/',
    codexDesc: 'AGENTS.md + .codex/',
    copilotDesc: '.github/copilot-instructions.md',
    kiroDesc: '.kiro/steering/dwyt.md',
    cursorDesc: '.cursor/rules/dwyt.mdc',
    opencodeDesc: 'opencode.json + AGENTS.md',
    variable: 'variável',
  },
} as const

export type Strings = Record<string, string>
