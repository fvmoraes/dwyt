# Requirements Document

## Introduction

Hoje o RTK rastreia economia de tokens de forma **global** (em `~/.rtk` ou no escopo global do binário). Quando o DWYT exibe métricas por projeto, um projeto que nunca rodou comandos via `rtk` não tem diretório `.rtk` próprio, então o cálculo estritamente por-projeto retorna vazio — e o card cai num fallback "global" rotulado.

Esta feature adiciona uma **opção** para o DWYT garantir que cada projeto integrado tenha o RTK inicializado por-projeto (`.rtk` no diretório do projeto), de modo que as métricas de "Tokens saved" e "% savings" reflitam o uso **real daquele repositório**, e não um total global. A opção é plug-and-play, idempotente, não-destrutiva, e degrada com clareza onde o RTK não estiver disponível (ex.: Windows sem binário upstream).

## Glossary

- **DWYT**: aplicação "Don't Waste Your Tokens" que coordena RTK, Codebase MCP, Obsidian MCP e Headroom.
- **RTK**: ferramenta de rastreamento de economia de tokens cujo estado por-projeto é criado por `rtk init`.
- **RTK_Per_Project**: opção de configuração (campo `rtk_per_project`) que controla a inicialização do RTK por-projeto.
- **Projeto_RTK** (`.rtk` do projeto): diretório/estado criado por `rtk init` (sem `--global`) dentro da raiz do projeto, que faz o RTK contabilizar comandos por-repositório.
- **Integração_de_Projeto**: fluxo `integrate.Project` executado quando o usuário roda `dwyt .` ou integra um projeto.
- **Setup_UI**: interface de configuração do DWYT onde o usuário ativa ou desativa opções.
- **Camada_de_Plataforma**: módulo existente responsável pela descoberta de executáveis e resolução de caminhos por sistema operacional.
- **Bloco_DWYT**: região delimitada por marcadores DWYT dentro de arquivos gerenciados (ex.: `.gitignore`), na qual o DWYT pode escrever sem tocar no conteúdo externo.
- **scope=project / scope=global**: rótulo existente no `ToolDetail` que indica se a métrica exibida é do projeto ou o total global.

## Requirements

### Requirement 1: Opção de configuração "RTK por projeto"

**User Story:** Como usuário do DWYT, quero uma opção que ative a inicialização do RTK por projeto, para que minhas métricas de economia sejam contabilizadas por repositório em vez de globalmente.

#### Acceptance Criteria

1. WHEN o usuário abre o Setup_UI, THE Setup_UI SHALL exibir um toggle "Inicializar RTK por projeto" com rótulo i18n EN/PT na seção de ferramentas do RTK.
2. WHEN o usuário ativa ou desativa o toggle e salva o setup, THE DWYT SHALL persistir a preferência no campo `rtk_per_project` da configuração do setup.
3. WHERE não existe preferência salva, THE DWYT SHALL adotar o padrão habilitado definido no Requirement 5 sem exigir edição manual de arquivos.
4. WHEN a preferência é lida em qualquer plataforma, THE DWYT SHALL tratar valor ausente como o padrão habilitado e retornar esse valor sem erro.

### Requirement 2: Garantir `.rtk` na integração do projeto

**User Story:** Como usuário, quero que ao integrar um projeto com a opção ligada o DWYT crie o `.rtk` automaticamente, para não precisar rodar `rtk init` manualmente.

#### Acceptance Criteria

1. WHEN RTK_Per_Project está habilitado AND a Integração_de_Projeto ocorre AND o binário `rtk` está instalado AND o projeto ainda não possui Projeto_RTK, THE DWYT SHALL executar `rtk init` no diretório do projeto.
2. IF o projeto já possui Projeto_RTK, THEN THE DWYT SHALL preservar o estado existente sem reinicializar nem sobrescrever, mantendo a operação idempotente.
3. IF a inicialização do Projeto_RTK falha, THEN THE DWYT SHALL registrar um aviso em log e continuar a Integração_de_Projeto até a conclusão.
4. WHILE RTK_Per_Project está desabilitado, THE DWYT SHALL deixar o projeto sem Projeto_RTK criado pela integração.
5. WHERE o binário `rtk` não está instalado, THE DWYT SHALL pular a etapa de inicialização e prosseguir a integração sem erro.
6. WHEN a Integração_de_Projeto cria o Projeto_RTK, THE DWYT SHALL adicionar a entrada `.rtk/` ao Bloco_DWYT do arquivo `.gitignore` do projeto, preservando todo o conteúdo fora do Bloco_DWYT.

### Requirement 3: Métricas refletem o escopo do projeto

**User Story:** Como usuário, quero ver no card do RTK as métricas reais do projeto quando o `.rtk` existir, para confiar que o número é daquele repositório.

#### Acceptance Criteria

1. WHEN o projeto possui Projeto_RTK, THE DWYT SHALL exibir as métricas do RTK com `scope=project` sem a nota de fallback global.
2. WHEN o projeto não possui Projeto_RTK, THE DWYT SHALL exibir o fallback global rotulado com `scope=global` e a respectiva nota.
3. WHEN o filtro de período Tudo, 1h, 6h, 24h, 2d ou 7d é aplicado, THE DWYT SHALL calcular e exibir as métricas para o escopo exibido.

### Requirement 4: Comportamento cross-platform

**User Story:** Como usuário de Windows, macOS ou Linux, quero que a opção se comporte de forma consistente e previsível na minha plataforma.

#### Acceptance Criteria

1. WHEN a plataforma é Linux ou macOS AND o binário `rtk` está instalado, THE DWYT SHALL inicializar o Projeto_RTK por projeto conforme o Requirement 2.
2. WHERE a plataforma é Windows sem binário `rtk` AND RTK_Per_Project está habilitado, THE DWYT SHALL pular a etapa de inicialização e exibir indicação de que o RTK por projeto não está disponível nesta plataforma.
3. WHEN o comando de init é invocado, THE DWYT SHALL usar a Camada_de_Plataforma existente para descoberta de executável e resolução de caminhos.

### Requirement 5: Padrão e migração

**User Story:** Como mantenedor, quero um padrão sensato e plug-and-play para a opção, para que as métricas reflitam o uso real por repositório sem exigir configuração manual.

#### Acceptance Criteria

1. THE DWYT SHALL definir o padrão da opção RTK_Per_Project como habilitado, alinhado à filosofia plug-and-play e às métricas reais por repositório.
2. WHEN uma instalação existente lê a configuração sem o campo `rtk_per_project`, THE DWYT SHALL adotar o padrão habilitado na leitura sem alterar projetos já inicializados.
3. WHEN RTK_Per_Project está habilitado, THE DWYT SHALL inicializar o Projeto_RTK nas próximas Integrações_de_Projeto sem afetar projetos que já possuem Projeto_RTK.
4. WHEN o usuário desabilita RTK_Per_Project, THE DWYT SHALL exibir o fallback global rotulado para projetos sem Projeto_RTK e SHALL preservar qualquer Projeto_RTK já criado.

### Requirement 6: Inicialização manual sob demanda

**User Story:** Como usuário com projetos já integrados, quero um botão para inicializar o RTK neste projeto manualmente, para habilitar métricas por repositório sem reintegrar tudo.

#### Acceptance Criteria

1. THE Setup_UI SHALL exibir uma ação "Inicializar RTK neste projeto" no card do RTK.
2. WHEN o usuário aciona a ação AND o binário `rtk` está instalado AND o projeto ainda não possui Projeto_RTK, THE DWYT SHALL executar `rtk init` no diretório do projeto e adicionar `.rtk/` ao Bloco_DWYT do `.gitignore`.
3. IF o projeto já possui Projeto_RTK quando a ação é acionada, THEN THE DWYT SHALL preservar o estado existente e informar que o RTK já está inicializado.
4. WHERE o binário `rtk` não está disponível na plataforma, THE Setup_UI SHALL exibir a ação como indisponível com indicação do motivo.
5. IF a inicialização manual falha, THEN THE DWYT SHALL exibir uma mensagem de erro descritiva e preservar o estado atual do projeto.
