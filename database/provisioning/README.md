# Provisionamento do PostgreSQL de produção

Estes scripts criam e desativam um usuário temporário capaz de executar
`database/bootstrap/002-roles.sql` e criar uma database adicional. Eles não são
embutidos no binário e não são executados pelo `docker-compose` local.

## 1. Criar o provisionador

Um DBA ou superusuário deve executar o script conectado à database na qual o
`002-roles.sql` será aplicado:

```shell
psql "$DBA_DATABASE_URL" -X \
  -f database/provisioning/create-memory-provisioner.sql
```

O `psql` solicitará a senha de `memory_provisioner` sem colocá-la no arquivo,
histórico do shell ou log SQL. O usuário expira automaticamente após 24 horas.

O provedor PostgreSQL precisa permitir que o administrador conceda `BYPASSRLS`.
Quando esse atributo é bloqueado pelo serviço gerenciado, o DBA do provedor
deve criar diretamente o role `memory_admin` definido em `002-roles.sql`.

## 2. Executar o bootstrap de roles

Conecte como `memory_provisioner`, obtendo a senha do gerenciador de segredos:

```shell
psql "$PROVISIONER_DATABASE_URL" -X -v ON_ERROR_STOP=1 \
  -f database/bootstrap/002-roles.sql
```

Os roles são globais ao cluster. Execute esse arquivo apenas uma vez, e não uma
vez por database.

## 3. Criar outra database

Conectado como `memory_provisioner`:

```sql
CREATE DATABASE outra_database;
```

O provisionador será o proprietário da database. Em seguida, conecte-se à nova
database e conceda as permissões locais de schema:

```sql
GRANT CREATE, USAGE ON SCHEMA public TO memory_migrator;
GRANT USAGE ON SCHEMA public TO memory_app, memory_admin;
```

As extensões são locais a cada database. O DBA deve executar também, conectado
à nova database:

```shell
psql "$DBA_NEW_DATABASE_URL" -X -v ON_ERROR_STOP=1 \
  -f database/bootstrap/001-extensions.sql
```

Depois disso, as migrations podem ser executadas como `memory_migrator`.

## 4. Desativar o provisionador

Ao terminar, o DBA deve conectar-se à database usada no passo 1 e executar:

```shell
psql "$DBA_DATABASE_URL" -X \
  -f database/provisioning/disable-memory-provisioner.sql
```

O role permanece registrado como grantor, mas perde login, senha, `CREATEDB`,
`CREATEROLE` e `BYPASSRLS`.


memory_app: 00abee5dc3d39700571259a0ea99458744adf31f6891a9fb7df5da9e4de1d199
memory_migrator: b2da54a3157ff2e14dde8aa6e73f394e77517f405a19531b4618b8d5d72743cb
memory_admin: 4bbe7772c2fa5b0294de21446d32380edf99a5fc9bb4085e85f6a1c863805e6c