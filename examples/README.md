# CacheGrid Examples

Runnable examples demonstrating CacheGrid features.

## Examples

### [basic/](basic/) — Core Features

Covers all fundamental operations: in-memory and disk caching, CRUD, TTL, atomic counters, cache-aside pattern, bulk ops, tags, and namespaces.

```bash
go run ./examples/basic
```

### [webapi/](webapi/) — REST API with Middleware

A bookstore API that uses CacheGrid for response caching, rate limiting, manual cache invalidation, and distributed locks to prevent race conditions on purchases.

```bash
go run ./examples/webapi

# In another terminal:
curl localhost:8080/books
curl localhost:8080/books/1
curl -X POST localhost:8080/books -d '{"title":"New Book","author":"Me","price":9.99}'
curl -X POST localhost:8080/books/1/purchase
```

### [sessions/](sessions/) — Persistent Session Store

A session management system using disk mode (PebbleDB). Sessions survive server restarts. Includes login, session validation, and logout.

```bash
go run ./examples/sessions

# In another terminal:
TOKEN=$(curl -s -X POST localhost:8080/login -d '{"username":"alice"}' | jq -r .token)
curl localhost:8080/me -H "Authorization: Bearer $TOKEN"
curl -X POST localhost:8080/logout -H "Authorization: Bearer $TOKEN"
```

### [pubsub/](pubsub/) — Real-Time Event Monitoring

Demonstrates the pub/sub system: pattern-based subscriptions, event hooks for metrics, eviction monitoring, and expiry events.

```bash
go run ./examples/pubsub
```

## Running All Examples

```bash
# From the project root
for dir in examples/*/; do
    echo "=== Running $dir ==="
    go run ./$dir
    echo
done
```
