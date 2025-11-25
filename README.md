## PG Setup

Требования: PG 15

Создание БД

```sql
CREATE DATABASE itmo_ledger;
```

Создание роли

```sql
CREATE ROLE itmo_ledger WITH LOGIN PASSWORD 'Secret123';
```

Передать права новому пользователю 

```sql
ALTER DATABASE itmo_ledger OWNER TO itmo_ledger;
```

Connection string подхватывается из переменной окружения DB_DSN или из флага cli db-dsn

```bash
DB_DSN=postgres://itmo_ledger:Secret123@localhost/itmo_ledger?sslmode=disable
```

Накатить миграции

```bash
migrate -path=./migrations -database=$DB_DSN up
```

## Запуск

ВАЖНО: Установить DB_DSN, см выше

```bash
go run ./cmd/api
```

## Примеры запросов

Добавление бонусных баллов с указанием срока жизни (в днях)
```bash
curl -X POST localhost:8080/v1/transactions -d '{"user_id": "653F535D-10BA-4186-A05B-74493354F13B", "amount": 100, "type": "deposit", "lifetime_days": 30}' 
```

Добавление бонусных баллов без указания срока (по умолчанию 365 дней)
```bash
curl -X POST localhost:8080/v1/transactions -d '{"user_id": "653F535D-10BA-4186-A05B-74493354F13B", "amount": 100, "type": "deposit"}' 
```

Списание бонусных баллов (FIFO - списываются самые старые баллы первыми)
```bash
curl -X POST localhost:8080/v1/transactions -d '{"user_id": "653F535D-10BA-4186-A05B-74493354F13B", "amount": 50, "type": "withdrawal"}' 
```

Получение баланса с информацией о сгорающих баллах в ближайшие 30 дней
```bash
curl -X GET localhost:8080/v1/users/653F535D-10BA-4186-A05B-74493354F13B/balance 
```

Пример ответа:
```json
{
  "user_id": "653f535d-10ba-4186-a05b-74493354f13b",
  "balance": 300,
  "expirations": {
    "2025-11-30": 100,
    "2025-12-07": 200
  }
}
```

## Особенности реализации

- **Персистентное хранение**: Все транзакции с бонусными баллами хранятся в PostgreSQL
- **Срок жизни баллов**: Каждая транзакция добавления баллов имеет срок истечения
- **FIFO списание**: При списании баллов первыми расходуются самые старые (те, которые скоро сгорят)
- **Консистентность**: Используется блокировка строк (`SELECT FOR UPDATE`) для обеспечения консистентности при параллельных списаниях
- **Информация об истечении**: API показывает сколько баллов сгорит в ближайшие 30 дней

## Тестирование

Для запуска тестов необходима тестовая база данных PostgreSQL.

### Настройка тестовой БД

Тесты используют переменную окружения `TEST_DATABASE_DSN` для подключения к PostgreSQL:

```bash
export TEST_DATABASE_DSN="postgres://itmo_ledger:Secret123@localhost/itmo_ledger_test?sslmode=disable"
```

### Подготовка базы данных

Создать тестовую базу данных и включить расширение pgcrypto (для генерации UUID):

```sql
CREATE DATABASE itmo_ledger_test;
ALTER DATABASE itmo_ledger_test OWNER TO itmo_ledger;
\c itmo_ledger_test
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

### Запуск PostgreSQL в Docker (для локальной разработки)

```bash
docker run --name postgres-test -e POSTGRES_USER=itmo_ledger -e POSTGRES_PASSWORD=Secret123 -e POSTGRES_DB=itmo_ledger_test -p 5432:5432 -d postgres:15

# Подключиться и создать расширение pgcrypto
docker exec -it postgres-test psql -U itmo_ledger -d itmo_ledger_test -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"
```

### Запуск тестов

```bash
go test ./...
```

Для запуска с подробным выводом:

```bash
go test -v ./...
```

### Примечание

Тесты автоматически применяют необходимые миграции при запуске. Если тестовая база данных не настроена или переменная `TEST_DATABASE_DSN` не установлена, тесты будут пропущены.
