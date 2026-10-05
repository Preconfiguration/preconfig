import test from "node:test";
import assert from "node:assert";
import pg from "pg";

test("the database answers", async () => {
  const client = new pg.Client({ connectionString: process.env.DATABASE_URL });
  await client.connect();
  const { rows } = await client.query("SELECT 1 AS one");
  assert.strictEqual(rows[0].one, 1);
  await client.end();
});
