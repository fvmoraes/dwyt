# Plano revisado - DWYT token savings, instrucoes das IAs e Kiro Power

Objetivo: implementar melhorias no DWYT para medir economia estimada de tokens em Codebase e Obsidian, reforcar o uso correto das ferramentas integradas nas instrucoes das IAs, preservar arquivos existentes em modo append-only seguro, atualizar a integracao do Kiro Power conforme a documentacao atual e manter instalacao/status/atualizacao coerentes entre CLI e UI.

## Premissas

- O usuario fara commit e push.
- Nenhum arquivo de instrucao/configuracao de IA deve ser sobrescrito integralmente quando ja existir.
- Blocos controlados pelo DWYT devem ser identificaveis, idempotentes e atualizaveis sem duplicacao.
- A UI deve continuar funcionando com os dados atuais de RTK e Headroom.
- A excecao do Codex autenticado via ChatGPT/OAuth deve continuar impedindo configuracao indevida do Headroom.
- O instalador via `curl ... | bash` deve sempre baixar a versao publicada mais recente e sobrescrever a versao anterior instalada.
- Estados exibidos por CLI e UI devem usar a mesma semantica: ferramentas sob demanda instaladas nao devem aparecer como falha.
- A Lei do Obsidian e obrigatoria para agentes, documentacao, templates gerados e vaults novos.

## 1. Tokens Saved - Codebase

Adicionar uma estimativa barata e consistente para `tokens_saved` no detalhe do `codebase-memory-mcp`.

Calculo proposto:

- usar os metadados ja conhecidos do indice (`nodes` e `edges`) quando o projeto estiver indexado;
- estimar o custo de leitura manual do repositorio a partir desses metadados;
- estimar o custo de consulta estrutural via MCP como um valor pequeno e fixo/proporcional;
- registrar `tokens_saved = max(manual_tokens - mcp_tokens, 0)`;
- expor tambem o custo estimado usado pelo DWYT para que o resumo global consiga calcular `Without DWYT` e `With DWYT`.

Validacao:

- card do Codebase mostra `Tokens Saved` quando houver indice;
- resumo global inclui a economia do Codebase;
- projetos sem indice continuam exibindo valores vazios, sem erro.

## 2. Tokens Saved - Obsidian

Adicionar `tokens_saved` ao card do Obsidian.

Calculo proposto:

- medir numero de arquivos markdown e bytes do vault do projeto;
- estimar tokens de contexto manual como `total_bytes / 4`;
- estimar custo de pesquisa/salvamento via MCP como um pequeno overhead proporcional aos arquivos;
- registrar `tokens_saved = max(manual_tokens - mcp_tokens, 0)`;
- atualizar a UI depois de salvar, buscar, resumir ou abrir dados relevantes do Obsidian.

Validacao:

- card do Obsidian mostra `Tokens Saved`;
- resumo global inclui a economia do Obsidian;
- vault vazio ou recem-criado nao gera numeros artificiais altos.

## 3. Lei do Obsidian - obrigatorio

O Obsidian e a memoria oficial do projeto. Toda interacao de agente deve seguir esta ordem:

1. Antes de agir, consultar o Obsidian para contexto:
   - buscar notas existentes via Obsidian MCP/API;
   - reconstruir ou ler o resumo atual do vault.
2. Durante a acao, salvar decisoes e status:
   - `type: "decision"` para ADRs e escolhas tecnicas;
   - `type: "task"` para tarefas, progresso e status.
3. Ao final de toda tarefa, salvar contexto completo com todos os campos:
   - `summary`;
   - `user_request`;
   - `files`;
   - `decisions`;
   - `actions`;
   - `commands`;
   - `errors`;
   - `outcome`;
   - `next_steps`;
   - `context`.

O vault do Obsidian deve ser rico, interligado e organizado, usando notas, folders, links internos, templates, instrucoes e mapas de navegacao. Todo contexto relevante deve ser salvo nele pelos agentes de IA.

Validacao:

- `docs/OBSIDIAN-LAW.md` documenta a regra completa;
- `AGENTS.md`, `CLAUDE.md`, Cursor, Kiro, Copilot e Kiro Power reforcam a obrigatoriedade;
- novos vaults nascem com `instructions/`, `templates/`, `maps/`, links internos e templates basicos;
- nenhuma tarefa e encerrada sem `obsidian_save_context`.

## 4. Dados reais vs estimativas locais

Manter dados reais quando ja existem:

- RTK continua vindo de `rtk gain`;
- Headroom continua vindo de `/stats`;
- Codebase e Obsidian usam estimativas locais ate haver telemetria nativa de MCP/harness.

As estimativas devem ser documentadas no codigo por nomes claros de helpers, nao por comentarios longos.

## 5. Arquivos de instrucao das IAs em modo append-only seguro

Atualizar a geracao de arquivos como:

- `AGENTS.md`
- `CLAUDE.md`
- `.cursor/rules/dwyt.mdc`
- `.kiro/steering/dwyt.md`
- `.github/copilot-instructions.md`

Regras:

- se o arquivo nao existir, criar com o bloco DWYT;
- se existir, preservar conteudo original;
- adicionar o bloco DWYT se ausente;
- atualizar somente o bloco DWYT se presente;
- nao duplicar blocos;
- nao remover conteudo fora do bloco controlado pelo DWYT.

Marcadores:

```md
<!-- dwyt:instructions:start -->
#dwyt
...
<!-- dwyt:instructions:end -->
```

## 6. Conteudo padrao das instrucoes DWYT

O bloco deve instruir as IAs a usar:

- Obsidian como primeira fonte de contexto persistente;
- Lei do Obsidian com consulta antes, salvamento durante e contexto completo ao final;
- Codebase MCP para descoberta estrutural antes de varrer o repositorio manualmente;
- RTK para comandos shell;
- Headroom quando compativel;
- excecao: Codex com login ChatGPT/OAuth nao deve usar Headroom;
- salvamento de contexto no Obsidian ao fim de tarefas relevantes.

O payload de contexto deve incluir:

- pedido do usuario;
- resumo;
- arquivos alterados;
- decisoes;
- acoes;
- comandos;
- erros;
- resultado;
- proximos passos.
- contexto para agentes futuros.

## 7. Kiro Power e configuracao MCP

Atualizar a integracao com Kiro conforme documentacao atual:

- todo Power deve ter `POWER.md` com frontmatter;
- `mcp.json` e `steering/` sao opcionais, mas o DWYT deve gerar ambos quando houver MCPs disponiveis;
- o Power local fica em `~/.dwyt/powers/dwyt-power`;
- o DWYT tenta registrar/linkar o Power em `~/.kiro/powers/dwyt-power`;
- quando a instalacao automatica nao puder ser garantida, a UI/status deve indicar o caminho para ativacao manual pelo Kiro IDE usando "Add power from Local Path";
- config MCP por workspace deve ser escrita em `.kiro/settings/mcp.json`;
- `.kiro/mcp.json` pode continuar sendo atualizado como compatibilidade legada, mas sem depender dele como fonte primaria.

O `POWER.md` deve conter frontmatter com:

- `name`;
- `displayName`;
- `description`;
- `keywords`;
- `author`.

Keywords minimas:

- `dwyt`
- `codebase`
- `obsidian`
- `mcp`
- `memory`
- `project memory`
- `token savings`
- `repo analysis`
- `arquitetura`
- `refatoracao`
- `debugging`
- `documentacao`
- `contexto do projeto`

## 8. Seguranca e nao regressao

Validar que:

- JSONs existentes fazem merge seguro e preservam MCPs do usuario;
- secoes DWYT nao duplicam;
- configs antigas de Kiro continuam aceitas quando existirem;
- Codex ChatGPT/OAuth continua sem Headroom;
- Kiro, Claude, Cursor, OpenCode, Copilot e Codex continuam recebendo instrucoes adequadas;
- a UI compila e exibe os novos valores;
- testes Go passam;
- build/lint do frontend e executado quando disponivel.

## 9. Instalacao, status e novas versoes

Garantir que o fluxo publico de instalacao e atualizacao seja previsivel:

- `curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash` sempre baixa a release mais recente;
- a instalacao sobrescreve com seguranca o binario antigo em `~/.local/bin/dwyt`;
- execucao via pipe nao reaproveita binario local do diretorio atual por acidente;
- `dwyt status` carrega o vault do projeto quando disponivel, igual ao daemon da UI;
- `installed (launch on demand)` e estados equivalentes aparecem como saudaveis ou inativos, nao como erro;
- a UI consulta a release mais recente publicada;
- quando existir versao nova, a UI mostra um aviso discreto com botao para abrir a instrucao de atualizacao;
- a instrucao exibida e o comando oficial de instalacao via `curl`.

Validacao:

- `dwyt status` condiz com os cards da UI para Codebase, RTK, Headroom e Obsidian;
- o aviso de nova versao nao aparece para builds `dev` nem quando a versao local ja e atual;
- falha de rede na consulta de release nao quebra a dashboard.

## 10. Resultado esperado

Ao fim da execucao:

- o plano esta consistente com a implementacao real;
- Codebase e Obsidian alimentam `tokens_saved`;
- o resumo global inclui Codebase e Obsidian;
- arquivos das IAs sao atualizados em modo append-only seguro;
- a Lei do Obsidian esta refletida nas docs, templates e vault seed;
- o Kiro Power segue a estrutura atual esperada;
- instalacao, status e atualizacao ficam consistentes entre CLI, UI e README;
- a validacao automatica foi executada;
- commit e push ficam a cargo do usuario.

## LEI DO CODEBASE — Mapa do Código é obrigatório

Ao trabalhar em qualquer projeto gerenciado pelo Dwyt, o agent **DEVE obrigatoriamente usar o MCP Codebase** sempre que precisar entender, validar ou alterar a estrutura real do código.

### Regra principal

- Antes de propor alterações, refatorações, correções ou diagnósticos técnicos, use o MCP Codebase para consultar o estado atual do projeto.
- Nunca assuma a estrutura do repositório apenas por memória, contexto anterior ou nomes de arquivos aparentes.
- O código atual indexado pelo Codebase é a fonte primária de verdade sobre arquivos, relações, dependências, símbolos, chamadas e caminhos.

### Ferramentas obrigatórias

Sempre prefira as ferramentas do MCP Codebase:

- `search_graph` para localizar arquivos, símbolos, módulos, serviços, handlers, componentes e relações.
- `trace_path` para entender fluxos, dependências, chamadas e impacto entre partes do sistema.
- `get_code_snippet` para ler trechos reais antes de sugerir ou aplicar mudanças.

### Proibições e restrições

- Evite `grep`, `glob`, `find`, leitura manual massiva de arquivos ou suposições sobre estrutura quando o MCP Codebase estiver disponível.
- Não altere arquivos críticos sem antes consultar o grafo do Codebase.
- Não crie código duplicado sem verificar se já existe implementação equivalente no projeto.
- Não remova, renomeie ou mova arquivos sem rastrear impacto via Codebase.

### Uso conjunto com Obsidian

Sempre que possível, use o MCP Codebase em conjunto com o MCP Obsidian:

- Codebase: entender a estrutura real e atual do código.
- Obsidian: recuperar decisões, histórico, planos, bugs conhecidos e contexto do projeto.

Ao final de uma tarefa relevante, atualize o Obsidian com informações úteis, como:

- decisões técnicas tomadas;
- arquivos alterados;
- problemas encontrados;
- próximos passos;
- links ou referências internas ao contexto do projeto;
- resumo do impacto da alteração.

### Fluxo obrigatório recomendado

1. Indexar ou validar o índice atual do projeto no Codebase.
2. Consultar `search_graph` para localizar a área afetada.
3. Usar `trace_path` para entender dependências e impacto.
4. Usar `get_code_snippet` antes de sugerir ou editar código.
5. Executar a alteração com cautela.
6. Validar se não houve regressão.
7. Registrar no Obsidian o que for relevante para memória futura do projeto.

### Princípio

> No Dwyt, nenhum agent deve trabalhar “no escuro”.
> Antes de mexer no código, consulte o mapa.
> Antes de esquecer o contexto, salve no Obsidian.

Duas lei principais, Obsidian e codebase, 

Sempre usar prioritariamente nessa ordem

RTK, Codebase, Obsdian e Headroom

Mostrar Tokens saved pelo codebase da mesma maneira que pelo o obsdian