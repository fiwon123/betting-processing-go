## Biblioteca para Banco de Dados

Foi utilizado o `pgx` com SQL explícito para o projeto.

Um driver considerado puro na linguagem Go e ser um kit de ferramenta robusto para o PostgresSQL.

Além de ser descrito como linguagem de baixo nível e de alta performance.

Checar mais no repositório a seguir:
https://github.com/jackc/pgx

## Mapeamento do Money

Money é representado como um valor inteiro usando a menor unidade, no caso do BRL o centavos.


Na API foi utilizado o tipo int64. E no banco de dados Postgres foi utilizado o tipo BIGINT.

Por exemplo:
- BRL **10.50** seria representado como **1050** em centavos.
- BRL **5.00** seria representado como **500** em centavos.

Os tipos flutuantes como float não são utilizados em valores monetários.
A variável **currency** é utilizado para identificação da moeda com base na **ISO 4217** como **BRL**.


## Dockerfile

Foi utilizado 3 etapas **Build**, **Test** e **Deploy** para manter consistência e reprodutibilidade.

Cada etapa fica separada, contendo apenas os arquivos necessários para sua execução.
Em caso de algum problema antes do **Deploy**, o processo é parado.
A etapa de **Test** automatiza e não depende de execução manual, assim mantendo integridade da etapa de **Build**.

## Docker Compose

Está sendo utilizado serviços separados para o Banco de Dados, Keycloak e a API.
Assim sendo mais fácil de desenvolver localmente sem precisar ter todas as dependências de instalação de certas bibliotecas e configurações iniciais. 

