import http from "node:http";
import pg from "pg";

const pool = new pg.Pool({ connectionString: process.env.DATABASE_URL });

export async function productCount() {
  const { rows } = await pool.query("SELECT count(*)::int AS n FROM products");
  return rows[0].n;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  http.createServer(async (_req, res) => {
    res.end(JSON.stringify({ products: await productCount() }));
  }).listen(3000);
}
