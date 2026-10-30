"""Inserts SYNTHETIC CPU/RAM history for every app so the charts have something to show.
Demo-only: real samples come from the panel's cgroup sampler. Usage: seed.py <brpanel.db>"""
import math, random, sqlite3, sys, time

db = sqlite3.connect(sys.argv[1])
random.seed(7)
now = int(time.time()); now -= now % 15
profiles = {"loja": (18, 420), "api": (35, 300), "worker": (55, 780)}
for (name,) in list(db.execute("select name from apps")):
    base_cpu, base_mem = profiles.get(name, (12, 200))
    rows = []
    for i in range(24 * 240):  # 24 h, one sample every 15 s
        ts = now - i * 15
        t = (now - ts) / 3600
        cpu = max(0.3, base_cpu * (1 + .6 * math.sin(t * 1.7)) + random.uniform(-3, 6) + (40 if int(t * 10) % 37 == 0 else 0))
        mem = (base_mem + 60 * math.sin(t / 3) - t * 3 + random.uniform(-4, 4)) * 1024 * 1024
        rows.append((name, ts, round(cpu, 1), int(mem)))
    db.executemany("insert or replace into metrics values (?,?,?,?)", rows)
db.commit()
