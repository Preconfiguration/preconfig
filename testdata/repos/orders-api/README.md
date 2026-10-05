# orders-api

A small orders service: orders in PostgreSQL, per-customer counts cached in Redis.
It is the sample repository of the Preconfiguration.com Alpha demo.

Run the tests with PostgreSQL 16 and Redis 7 on localhost:

    python3 -m venv .venv
    .venv/bin/pip install -r requirements.txt
    .venv/bin/pytest -q
