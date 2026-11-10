import os, socket, threading, time

# Reproduce the local-leg .out path: one end paced 640B writes every 20ms
# (daemon pacer), the other reads 640B (CLI drain). AF_UNIX SOCK_STREAM on
# macOS. If the reader's inter-read gaps blow past 20ms, the kernel is
# coalescing the drip - jitter is in socket transport, not our loops.

PAIR = 300  # 6 s of audio
CHUNK = 640

a, b = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)

def writer():
    payload = b"\x00" * CHUNK
    t0 = time.monotonic()
    for i in range(PAIR):
        a.sendall(payload)
        nxt = t0 + (i + 1) * 0.020
        d = nxt - time.monotonic()
        if d > 0:
            time.sleep(d)
    a.close()

th = threading.Thread(target=writer)
th.start()
b.setblocking(True)
gaps = []
last = None
maxr = 0
while True:
    d = b.recv(CHUNK)
    if not d:
        break
    now = time.monotonic()
    if last is not None:
        g = (now - last) * 1000
        gaps.append(g)
        maxr = max(maxr, len(d))
    last = now
th.join()
n = len(gaps)
if n:
    print(f"reads={n+1} max_read_bytes={maxr}")
    print(f"gap avg={sum(gaps)/n:.1f}ms p50={sorted(gaps)[n//2]:.1f}ms p95={sorted(gaps)[int(n*0.95)]:.1f}ms max={max(gaps):.1f}ms over60ms={sum(1 for g in gaps if g>60)}")
else:
    print("no gaps")
