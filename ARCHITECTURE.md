## Biblioteca para Banco de Dados

Foi utilizado o `pgx` com SQL explícito para o projeto.

Um driver considerado puro na linguagem Go e ser um kit de ferramenta robusto para o PostgresSQL.

Além de ser descrito como linguagem de baixo nível e de alta performance.

Checar mais no repositório a seguir:
https://github.com/jackc/pgx


## Dockerfile

Foi utilizado 3 etapas **Build**, **Test** e **Deploy** para manter consistência e reprodutibilidade.

Cada etapa fica separada, contendo apenas os arquivos necessários para sua execução.
Em caso de algum problema antes do **Deploy**, o processo é parado.
A etapa de **Test** automatiza e não depende de execução manual, assim mantendo integridade da etapa de **Build**.

## Docker Compose

Está sendo utilizado serviços separados para o Banco de Dados, Keycloak e a API.
Assim sendo mais fácil de desenvolver localmente sem precisar ter todas as dependências de instalação de certas bibliotecas e configurações iniciais. 

