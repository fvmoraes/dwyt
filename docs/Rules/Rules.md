# DWYT Token Savings, Leis dos Agentes e Kiro Power

## Objetivo

Implementar melhorias no DWYT para:

- exibir `Tokens Saved` no Codebase da mesma forma que já é exibido no Obsidian;
- incluir Codebase e Obsidian no resumo global de economia de tokens;
- reforçar as duas leis principais dos agentes: **Lei do Codebase** e **Lei do Obsidian**;
- configurar corretamente as instruções das IAs sem sobrescrever conteúdo do usuário;
- corrigir e validar a integração do Kiro Power;
- manter CLI, UI, instalador, status e documentação coerentes entre si;
- evitar regressões, duplicações e conflitos entre RTK, Codebase, Obsidian e Headroom.

---

## 0. Regra de prioridade das ferramentas

O DWYT deve orientar os agentes a usar as ferramentas nesta ordem de prioridade, **quando aplicável ao tipo de tarefa**:

1. **RTK** — prioridade para comandos shell, automações de terminal e comandos repetitivos.
2. **Codebase MCP** — prioridade para entender a estrutura real do código, dependências, símbolos, fluxos e impacto de alterações.
3. **Obsidian MCP** — prioridade para recuperar e salvar memória persistente do projeto, decisões, histórico, tarefas e contexto futuro.
4. **Headroom** — prioridade apenas como otimização de proxy/cache quando compatível com o cliente de IA.

Essa ordem não deve gerar conflito entre as ferramentas:

- RTK não substitui Codebase nem Obsidian; ele apenas otimiza comandos shell.
- Codebase é a fonte principal para estrutura atual do código.
- Obsidian é a fonte principal para memória, histórico e decisões do projeto.
- Headroom é otimização de tráfego, não fonte de verdade.
- Codex autenticado via ChatGPT/OAuth **não deve usar Headroom**.

---

## 1. Premissas obrigatórias

- O usuário fará commit e push manualmente.
- O DWYT não deve sobrescrever integralmente arquivos de instrução ou configuração de IA já existentes.
- Blocos controlados pelo DWYT devem ser identificáveis, idempotentes e atualizáveis sem duplicação.
- Conteúdo fora dos blocos controlados pelo DWYT deve ser preservado.
- A UI deve continuar funcionando com os dados atuais de RTK e Headroom.
- Dados do Obsidian devem ficar sob `~/.dwyt` e ter persistência absoluta.
- Nenhum fluxo de install, uninstall, reinstall, clean, repair ou reset pode deletar vaults, projetos, notas ou histórico do Obsidian.
- Estados exibidos por CLI e UI devem usar a mesma semântica.
- Ferramentas instaladas sob demanda não devem aparecer como falha quando estiverem apenas inativas.
- As leis do Codebase e do Obsidian são obrigatórias para agentes, documentação, templates gerados, Kiro Power e novos vaults.

---

## 2. Lei do Codebase — mapa do código obrigatório

Ao trabalhar em qualquer projeto gerenciado pelo DWYT, o agente deve usar o **MCP Codebase** sempre que precisar entender, validar, diagnosticar ou alterar a estrutura real do código.

### 2.1 Regra principal

Antes de propor alterações, refatorações, correções ou diagnósticos técnicos:

- validar se o projeto está indexado;
- consultar o estado atual do código via Codebase MCP;
- evitar suposições baseadas apenas em memória, contexto anterior ou nomes aparentes de arquivos.

O código indexado pelo Codebase é a fonte primária para:

- arquivos;
- relações;
- dependências;
- símbolos;
- chamadas;
- caminhos;
- impacto de alterações.

### 2.2 Ferramentas preferenciais

Sempre preferir:

- `search_graph` para localizar arquivos, módulos, símbolos, serviços, handlers, componentes e relações;
- `trace_path` para entender fluxos, chamadas, dependências e impacto;
- `get_code_snippet` para ler trechos reais antes de sugerir ou aplicar mudanças.

### 2.3 Restrições

Quando o Codebase MCP estiver disponível:

- evitar `grep`, `glob`, `find` e leitura manual massiva como primeira estratégia;
- não alterar arquivos críticos sem consultar o grafo;
- não criar código duplicado sem verificar se já existe implementação equivalente;
- não remover, renomear ou mover arquivos sem rastrear impacto.

### 2.4 Fluxo recomendado

1. Validar ou atualizar o índice do projeto.
2. Usar `search_graph` para localizar a área afetada.
3. Usar `trace_path` para entender dependências e impacto.
4. Usar `get_code_snippet` antes de sugerir ou editar código.
5. Aplicar a alteração com cautela.
6. Validar build, testes e comportamento.
7. Registrar o contexto relevante no Obsidian.

---

## 3. Lei do Obsidian — memória persistente obrigatória

O Obsidian é a memória oficial do projeto dentro do DWYT.

O agente deve usar o **MCP Obsidian** para recuperar contexto antes de tarefas relevantes e salvar informações úteis durante ou ao final da execução.

### 3.1 Antes de agir

Antes de iniciar tarefas de diagnóstico, refatoração, planejamento, documentação ou alteração relevante:

- buscar notas existentes no vault do projeto;
- ler ou reconstruir o resumo atual;
- recuperar decisões, bugs conhecidos, tarefas abertas e histórico relevante.

### 3.2 Durante a ação

Durante a execução, salvar informações quando houver mudança relevante:

- decisões técnicas;
- status de tarefa;
- problemas encontrados;
- hipóteses confirmadas ou descartadas;
- comandos importantes;
- arquivos impactados.

Tipos sugeridos:

- `type: "decision"` para ADRs e escolhas técnicas;
- `type: "task"` para tarefas, progresso e status;
- `type: "debug"` para erros, investigação e causa raiz;
- `type: "context"` para resumo útil a agentes futuros.

### 3.3 Ao final da tarefa

Ao final de toda tarefa relevante, salvar um contexto completo com:

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

Se o Obsidian MCP estiver indisponível, o agente deve:

- não travar a tarefa;
- registrar a falha de forma clara;
- orientar o usuário sobre como reexecutar o salvamento depois;
- nunca apagar ou recriar vaults como tentativa de correção automática.

### 3.4 Estrutura dos vaults

Novos vaults devem nascer organizados com:

- `instructions/`;
- `templates/`;
- `maps/`;
- `decisions/`;
- `tasks/`;
- `debug/`;
- `context/`.

Também devem incluir:

- links internos;
- templates básicos;
- mapa de navegação;
- instruções de uso para agentes;
- referência à Lei do Obsidian e à Lei do Codebase.
- TODOS os arquivos do obsidian em cada vault, devem ser criados com interligação e tags [[]], nada de arquivos soltos, um mapa deve ser estruturado.
---

## 4. Uso conjunto: RTK, Codebase, Obsidian e Headroom

As ferramentas devem ser usadas de forma complementar.

### 4.1 RTK

Usar RTK para:

- comandos shell frequentes;
- compressão de comandos longos;
- execução padronizada de tarefas repetitivas;
- economia de tokens em interações de terminal.

A métrica de economia do RTK continua vindo de `rtk gain`.

### 4.2 Codebase

Usar Codebase para:

- análise estrutural;
- navegação pelo grafo do projeto;
- localização de símbolos e dependências;
- prevenção de leitura manual massiva do repositório.

### 4.3 Obsidian

Usar Obsidian para:

- contexto persistente;
- decisões;
- tarefas;
- histórico;
- documentação incremental;
- memória para agentes futuros.

### 4.4 Headroom

Usar Headroom apenas quando compatível.

Regras:

- manter dados reais vindos de `/stats`;
- não usar com Codex autenticado via ChatGPT/OAuth;
- não apresentar Headroom como obrigatório quando o cliente não suportar proxy/base URL;
- falha no Headroom não deve quebrar Codebase, RTK ou Obsidian.

---

## 5. Tokens Saved — Codebase

Adicionar uma estimativa barata, consistente, realista e transparente para `tokens_saved` no detalhe do `codebase-memory-mcp`.

### 5.1 Cálculo proposto

Quando o projeto estiver indexado:

- usar metadados já conhecidos do índice, como `nodes` e `edges`;
- estimar o custo de leitura manual do repositório com base nesses metadados;
- estimar o custo de consulta estrutural via MCP como valor pequeno, fixo ou proporcional;
- calcular:

```txt
tokens_saved = max(manual_tokens - mcp_tokens, 0)
```

Também expor os campos usados no resumo global:

- `without_dwyt_tokens`;
- `with_dwyt_tokens`;
- `tokens_saved`;
- `estimation_source`.

### 5.2 Regras de exibição

- O card do Codebase deve mostrar `Tokens Saved` usando o mesmo padrão visual do card do Obsidian.
- O resumo global deve incluir a economia do Codebase.
- Projetos sem índice não devem exibir erro.
- Projetos sem índice podem exibir `—`, `0` ou estado neutro, desde que a UI seja consistente com Obsidian.
- A estimativa deve deixar claro que é local até existir telemetria nativa do MCP ou harness.

### 5.3 Validação

Validar que:

- o card do Codebase mostra `Tokens Saved` quando houver índice;
- o valor entra no resumo global;
- não há números artificiais altos em projetos pequenos ou recém-indexados;
- não há regressão no status atual do Codebase;
- a UI não quebra quando o índice estiver ausente, vazio ou corrompido.

---

## 6. Tokens Saved — Obsidian

Adicionar ou revisar `tokens_saved` no card do Obsidian para manter paridade com o Codebase.

### 6.1 Cálculo proposto

Para o vault do projeto:

- medir quantidade de arquivos Markdown;
- medir bytes totais do vault relevante;
- estimar tokens de contexto manual como `total_bytes / 4`;
- estimar overhead de busca/salvamento via MCP como custo pequeno proporcional;
- calcular:

```txt
tokens_saved = max(manual_tokens - mcp_tokens, 0)
```

### 6.2 Atualização da UI

Atualizar os dados depois de ações relevantes:

- salvar contexto;
- buscar notas;
- resumir vault;
- abrir vault;
- criar vault;
- reindexar ou recalcular status do projeto.

### 6.3 Validação

Validar que:

- o card do Obsidian mostra `Tokens Saved`;
- o resumo global inclui a economia do Obsidian;
- vault vazio ou recém-criado não gera economia artificial alta;
- falhas de leitura do vault não quebram a dashboard;
- nenhum processo apaga dados do vault.

---

## 7. Dados reais vs estimativas locais

Manter dados reais quando já existirem:

- RTK continua vindo de `rtk gain`;
- Headroom continua vindo de `/stats`;
- Codebase usa estimativa local até haver telemetria nativa;
- Obsidian usa estimativa local até haver telemetria nativa.

As estimativas devem ser implementadas com helpers claros, por exemplo:

- `estimateCodebaseTokenSavings`;
- `estimateObsidianTokenSavings`;
- `calculateGlobalTokenSavings`.

Evitar comentários longos no código. A documentação da fórmula deve ficar em docs ou README técnico.

---

## 8. Arquivos de instrução das IAs — append-only seguro

Atualizar a geração dos arquivos:

- `AGENTS.md`;
- `CLAUDE.md`;
- `.cursor/rules/dwyt.mdc`;
- `.kiro/steering/dwyt.md`;
- `.github/copilot-instructions.md`;
- arquivos equivalentes de OpenCode, Codex e outros clientes suportados.

### 8.1 Regras obrigatórias

- Se o arquivo não existir, criar com o bloco DWYT.
- Se o arquivo existir, preservar o conteúdo original.
- Se o bloco DWYT estiver ausente, adicionar o bloco.
- Se o bloco DWYT já existir, atualizar somente o bloco.
- Não duplicar blocos.
- Não remover conteúdo fora do bloco controlado pelo DWYT.
- Não alterar configurações manuais do usuário fora da seção DWYT.

### 8.2 Marcadores oficiais

Usar marcadores únicos e estáveis:

```md
<!-- dwyt:instructions:start -->
# DWYT
...
<!-- dwyt:instructions:end -->
```

### 8.3 Conteúdo do bloco DWYT

O bloco deve sem super completo e instruir as IAs a:

- usar RTK para comandos shell quando aplicável;
- usar Codebase MCP antes de analisar ou alterar estrutura real de código;
- usar Obsidian MCP para recuperar e salvar memória persistente;
- usar Headroom somente quando compatível;
- nunca usar Headroom com Codex autenticado via ChatGPT/OAuth;
- salvar contexto no Obsidian ao final de tarefas relevantes;
- respeitar a ordem de prioridade: RTK, Codebase, Obsidian e Headroom;
- evitar sobrescrever arquivos de configuração do usuário;
- validar alterações antes de concluir.
- Ter as leis do Codebase e Obsidian bem explicitas e completas

### 8.4 Payload mínimo de contexto

O payload salvo no Obsidian deve conter:

- pedido do usuário;
- resumo;
- arquivos alterados;
- decisões;
- ações;
- comandos;
- erros;
- resultado;
- próximos passos;
- contexto para agentes futuros.

TUDO interligado [[]]

---

## 9. Kiro Power e configuração MCP

Atualizar a integração com Kiro com validação contra a documentação oficial atual antes da implementação final.

### 9.1 Estrutura esperada

- Todo Power deve ter `POWER.md` com frontmatter.
- O Power local do DWYT deve ficar em:

```txt
~/.dwyt/powers/dwyt-power
```

- O DWYT deve tentar registrar ou linkar o Power em:

```txt
~/.kiro/powers/dwyt-power
```

- Quando a instalação automática não puder ser garantida, a UI/status deve mostrar instrução de ativação manual usando o caminho local do Power.

### 9.2 Configuração MCP do Kiro

- A configuração MCP por workspace deve ser escrita em:

```txt
.kiro/settings/mcp.json
```

- `.kiro/mcp.json` pode ser atualizado por compatibilidade legada, mas não deve ser a fonte primária.
- JSONs existentes devem receber merge seguro.
- MCPs do usuário devem ser preservados.
- Entradas DWYT devem ser idempotentes.

### 9.3 Frontmatter mínimo do `POWER.md`

```yaml
---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT integration for Codebase MCP, Obsidian memory, RTK command compression and compatible Headroom usage.
keywords:
  - dwyt
  - codebase
  - obsidian
  - mcp
  - memory
  - project memory
  - token savings
  - repo analysis
  - arquitetura
  - refatoracao
  - debugging
  - documentacao
  - contexto do projeto
author: DWYT
---
```

### 9.4 Conteúdo do Power

O Power deve reforçar:

- Lei do Codebase;
- Lei do Obsidian;
- prioridade RTK, Codebase, Obsidian e Headroom;
- exceção Codex ChatGPT/OAuth sem Headroom;
- uso obrigatório de MCPs quando disponíveis;
- salvamento de contexto ao final da tarefa.

---

## 10. Segurança e não regressão

Validar que:

- JSONs existentes fazem merge seguro;
- MCPs do usuário são preservados;
- seções DWYT não duplicam;
- configs antigas de Kiro continuam aceitas quando existirem;
- Codex ChatGPT/OAuth continua sem Headroom;
- Kiro, Claude, Cursor, OpenCode, Copilot e Codex recebem instruções adequadas;
- UI compila e exibe os novos valores;
- testes Go passam;
- build/lint do frontend é executado quando disponível;
- nenhum processo remove vaults ou dados persistentes do Obsidian;
- falha em uma ferramenta não derruba o status das demais;
- estados `installed`, `inactive`, `launch on demand` e equivalentes não são tratados como erro.

---

## 11. Instalação, status e novas versões

Garantir que o fluxo público de instalação e atualização seja previsível.

### 11.1 Instalador

O comando oficial:

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

Deve:

- baixar a release publicada mais recente;
- sobrescrever com segurança o binário antigo em `~/.local/bin/dwyt`;
- não reaproveitar binário local do diretório atual por acidente;
- preservar dados persistentes em `~/.dwyt`;
- nunca apagar vaults do Obsidian.

### 11.2 Status CLI e UI

`dwyt status` e dashboard devem usar a mesma semântica para:

- Codebase;
- RTK;
- Headroom;
- Obsidian.

Regras:

- `installed (launch on demand)` deve aparecer como saudável ou inativo, não como erro;
- `inactive` não deve ser tratado como falha crítica;
- ausência de índice deve ser estado neutro, não crash;
- falha de rede na consulta de release não deve quebrar a dashboard.

### 11.3 Aviso de versão nova

A UI deve:

- consultar a release mais recente publicada;
- mostrar aviso discreto quando houver versão nova;
- não mostrar aviso em builds `dev`;
- não mostrar aviso quando a versão local já for atual;
- exibir o comando oficial de atualização via `curl`.

---

## 12. Documentação a atualizar

Atualizar ou criar:

- `docs/OBSIDIAN-LAW.md`;
- `docs/CODEBASE-LAW.md`;
- documentação de `Tokens Saved`;
- documentação do Kiro Power;
- README principal;
- templates de agentes;
- seed inicial de vaults;
- instruções de instalação e atualização.

A documentação deve deixar claro:

- o que é dado real;
- o que é estimativa local;
- quando cada ferramenta deve ser usada;
- como evitar conflitos entre ferramentas;
- onde os dados são persistidos;
- quais dados nunca podem ser apagados automaticamente.

---

## 13. Validação final obrigatória

Executar validação minuciosa antes de concluir.

### 13.1 Backend

Validar:

- testes Go;
- handlers de status;
- cálculo de `tokens_saved`;
- serialização dos campos novos;
- segurança dos paths em `~/.dwyt`;
- preservação dos vaults;
- merge seguro de JSONs.

### 13.2 Frontend

Validar:

- build;
- lint quando disponível;
- cards de Codebase e Obsidian;
- resumo global;
- estados de erro, vazio, inativo e instalado sob demanda;
- aviso de nova versão;
- consistência visual dos cards.

### 13.3 Integrações

Validar:

- Kiro Power;
- `.kiro/settings/mcp.json`;
- `.kiro/mcp.json` legado;
- `AGENTS.md`;
- `CLAUDE.md`;
- Cursor;
- Copilot;
- OpenCode;
- Codex;
- exceção do Codex ChatGPT/OAuth sem Headroom.

### 13.4 Regressão

Confirmar que:

- nada apagou dados do Obsidian;
- blocos DWYT não duplicaram;
- conteúdo manual do usuário foi preservado;
- status da CLI bate com status da UI;
- ferramentas sob demanda não aparecem como falha;
- o instalador continua funcional;
- o README não contradiz a implementação.

---

## 14. Resultado esperado

Ao final da execução:

- Codebase mostra `Tokens Saved` com o mesmo padrão do Obsidian;
- Obsidian mantém `Tokens Saved` coerente e sem valores artificiais;
- resumo global inclui RTK, Headroom, Codebase e Obsidian;
- Lei do Codebase está documentada e aplicada nos templates;
- Lei do Obsidian está documentada e aplicada nos templates;
- ordem RTK, Codebase, Obsidian e Headroom está clara e sem conflito;
- arquivos de instrução das IAs são atualizados em modo append-only seguro;
- Kiro Power segue a estrutura esperada e preserva configurações existentes;
- instalação, atualização, status CLI e dashboard usam a mesma semântica;
- documentação está coerente com o comportamento real;
- validação automatizada foi executada;
- commit e push ficam a cargo do usuário.

---

## 15. Princípio final

> No DWYT, nenhum agente deve trabalhar no escuro.  
> Para comandos, use RTK.  
> Para entender código, consulte o Codebase.  
> Para lembrar contexto, use o Obsidian.  
> Para otimizar chamadas compatíveis, use Headroom.  
> Antes de mexer no código, consulte o mapa.  
> Antes de perder contexto, salve na memória do projeto.