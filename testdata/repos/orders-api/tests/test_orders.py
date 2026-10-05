import uuid

import psycopg
import pytest

from app import orders


@pytest.fixture
def conn():
    with orders.connect() as c:
        orders.migrate(c)
        yield c
        c.rollback()


@pytest.fixture
def r():
    client = orders.cache()
    yield client
    client.close()


def customer():
    return "c-" + uuid.uuid4().hex[:8]


def test_place_order_returns_an_id(conn, r):
    assert orders.place_order(conn, r, customer(), 1250) > 0


def test_count_is_cached_in_redis(conn, r):
    c = customer()
    orders.place_order(conn, r, c, 500)
    assert orders.order_count(conn, r, c) == 1
    assert r.get(f"orders:count:{c}") == "1"


def test_new_order_clears_the_cached_count(conn, r):
    c = customer()
    orders.place_order(conn, r, c, 500)
    assert orders.order_count(conn, r, c) == 1
    orders.place_order(conn, r, c, 700)
    assert orders.order_count(conn, r, c) == 2


def test_negative_totals_are_rejected(conn, r):
    with pytest.raises(psycopg.errors.CheckViolation):
        orders.place_order(conn, r, customer(), -1)
