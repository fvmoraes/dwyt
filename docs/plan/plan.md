# Plano revisado - DWYT token savings, instrucoes das IAs e Kiro Power

Objetivo: implementar melhorias no DWYT para medir economia estimada de tokens em Codebase e Obsidian, reforcar o uso correto das ferramentas integradas nas instrucoes das IAs, preservar arquivos existentes em modo append-only seguro e atualizar a integracao do Kiro Power conforme a documentacao atual.

## Premissas

- O usuario fara commit e push.
- Nenhum arquivo de instrucao/configuracao de IA deve ser sobrescrito integralmente quando ja existir.
- Blocos controlados pelo DWYT devem ser identificaveis, idempotentes e atualizaveis sem duplicacao.
- A UI deve continuar funcionando com os dados atuais de RTK e Headroom.
- A excecao do Codex autenticado via ChatGPT/OAuth deve continuar impedindo configuracao indevida do Headroom.

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

## 3. Dados reais vs estimativas locais

Manter dados reais quando ja existem:

- RTK continua vindo de `rtk gain`;
- Headroom continua vindo de `/stats`;
- Codebase e Obsidian usam estimativas locais ate haver telemetria nativa de MCP/harness.

As estimativas devem ser documentadas no codigo por nomes claros de helpers, nao por comentarios longos.

## 4. Arquivos de instrucao das IAs em modo append-only seguro

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

## 5. Conteudo padrao das instrucoes DWYT

O bloco deve instruir as IAs a usar:

- Obsidian como primeira fonte de contexto persistente;
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
- proximos passos.

## 6. Kiro Power e configuracao MCP

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

## 7. Seguranca e nao regressao

Validar que:

- JSONs existentes fazem merge seguro e preservam MCPs do usuario;
- secoes DWYT nao duplicam;
- configs antigas de Kiro continuam aceitas quando existirem;
- Codex ChatGPT/OAuth continua sem Headroom;
- Kiro, Claude, Cursor, OpenCode, Copilot e Codex continuam recebendo instrucoes adequadas;
- a UI compila e exibe os novos valores;
- testes Go passam;
- build/lint do frontend e executado quando disponivel.

## 8. Resultado esperado

Ao fim da execucao:

- o plano esta consistente com a implementacao real;
- Codebase e Obsidian alimentam `tokens_saved`;
- o resumo global inclui Codebase e Obsidian;
- arquivos das IAs sao atualizados em modo append-only seguro;
- o Kiro Power segue a estrutura atual esperada;
- a validacao automatica foi executada;
- commit e push ficam a cargo do usuario.
