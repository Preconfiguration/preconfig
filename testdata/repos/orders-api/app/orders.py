"""Orders: stored in PostgreSQL, with per-customer counts cached in Redis."""
import os

import psycopg
import redis

SCHEMA = """
CREATE TABLE IF NOT EXISTS orders (
    id serial PRIMARY KEY,
    customer text NOT NULL,
    total_cents integer NOT NULL CHECK (total_cents >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
)
"""


def connect() -> psycopg.Connection:
    return psycopg.connect(os.environ["DATABASE_URL"])


def cache() -> redis.Redis:
    return redis.Redis.from_url(os.environ["REDIS_URL"], decode_responses=True)


def migrate(conn: psycopg.Connection) -> None:
    conn.execute(SCHEMA)


def place_order(conn: psycopg.Connection, r: redis.Redis, customer: str, total_cents: int) -> int:
    row = conn.execute(
        "INSERT INTO orders (customer, total_cents) VALUES (%s, %s) RETURNING id",
        (customer, total_cents),
    ).fetchone()
    r.delete(f"orders:count:{customer}")
    return row[0]


def order_count(conn: psycopg.Connection, r: redis.Redis, customer: str) -> int:
    key = f"orders:count:{customer}"
    cached = r.get(key)
    if cached is not None:
        return int(cached)
    n = conn.execute("SELECT count(*) FROM orders WHERE customer = %s", (customer,)).fetchone()[0]
    r.set(key, n, ex=60)
    return n
