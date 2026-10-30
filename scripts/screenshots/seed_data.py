"""Puts sample rows into the databases the panel just provisioned (real servers), so the "Dados" tab has
something to show. Usage: seed_data.py <brpanel.db>. Missing client tools are skipped silently."""
import glob, shutil, sqlite3, subprocess, sys

db = sqlite3.connect(sys.argv[1])
rows = db.execute("select name, kind, port, username, password, dbname from databases where status = 'ready'").fetchall()

def run(cmd, env=None, stdin=None):
    try:
        subprocess.run(cmd, check=True, capture_output=True, env=env, input=stdin, text=True, timeout=60)
    except Exception as e:  # sample data is optional
        print("seed:", cmd[0], "falhou:", getattr(e, "stderr", e), file=sys.stderr)

for name, kind, port, user, pw, dbname in rows:
    if kind == "postgres":
        psql = (glob.glob("/usr/lib/postgresql/*/bin/psql") or [shutil.which("psql")])[0]
        url = f"postgresql://{user}:{pw}@127.0.0.1:{port}/{dbname}"
        run([psql, url, "-X", "-c", """
CREATE TABLE IF NOT EXISTS clientes (id serial PRIMARY KEY, nome text NOT NULL, email text, plano text, criado_em timestamptz DEFAULT now());
INSERT INTO clientes (nome, email, plano) VALUES
  ('Ana Souza','ana@exemplo.com','pro'), ('Bruno Lima','bruno@exemplo.com','free'), ('Carla Dias','carla@exemplo.com','pro'),
  ('Diego Rocha','diego@exemplo.com','business'), ('Elisa Prado', NULL, 'free');
CREATE TABLE IF NOT EXISTS pedidos (id serial PRIMARY KEY, cliente_id int REFERENCES clientes(id), total numeric(10,2), status text);
INSERT INTO pedidos (cliente_id, total, status) VALUES (1, 199.90, 'pago'), (1, 49.00, 'pendente'), (3, 899.00, 'pago');"""])
    elif kind == "redis" and shutil.which("redis-cli"):
        env = {"REDISCLI_AUTH": pw, "PATH": "/usr/bin:/bin"}
        for args in (["SET", "sessao:ana", "ativa", "EX", "3600"], ["HSET", "usuario:1", "nome", "Ana", "plano", "pro"],
                     ["RPUSH", "fila:emails", "boas-vindas", "fatura"], ["SADD", "tags", "novo", "vip"]):
            run(["redis-cli", "-h", "127.0.0.1", "-p", str(port), "--no-auth-warning", *args], env=env)
    elif kind == "mysql":
        client = shutil.which("mariadb") or shutil.which("mysql")
        if client:
            run([client, "--no-defaults", "-h", "127.0.0.1", "-P", str(port), "-u", user, dbname, "-e",
                 "CREATE TABLE IF NOT EXISTS produtos (id INT AUTO_INCREMENT PRIMARY KEY, nome VARCHAR(60), preco DECIMAL(8,2));"
                 "INSERT INTO produtos (nome, preco) VALUES ('Camiseta', 59.90), ('Caneca', 29.90), ('Boné', 39.00);"],
                env={"MYSQL_PWD": pw, "PATH": "/usr/bin:/bin"})
