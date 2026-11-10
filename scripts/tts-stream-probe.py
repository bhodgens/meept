import json, socket, sys, time

BASE = "socrates"
PORT = 8001
TEXT = sys.argv[1] if len(sys.argv) > 1 else "Hello, this is a longer greeting so there are plenty of chunks to observe."

body = json.dumps({"model": "qwen3-tts-1.7b", "voice": "serena", "input": TEXT,
                   "response_format": "pcm", "stream": True}).encode()
s = socket.create_connection((BASE, PORT), timeout=120)
req = (f"POST /v1/audio/speech HTTP/1.1\r\nHost: {BASE}\r\nContent-Type: application/json\r\n"
       f"Content-Length: {len(body)}\r\nConnection: close\r\n\r\n").encode() + body
t0 = time.time()
s.sendall(req)
chunks = []
hdr_done = False
buf = b""
while True:
    try:
        d = s.recv(65536)
    except Exception as e:
        print("recv err", e)
        break
    if not d:
        break
    if not hdr_done:
        buf += d
        if b"\r\n\r\n" in buf:
            hdr, rest = buf.split(b"\r\n\r\n", 1)
            hdr_done = True
            print("status line:", hdr.decode(errors="replace").splitlines()[0])
            if rest:
                chunks.append((time.time() - t0, len(rest)))
            buf = b""
    else:
        chunks.append((time.time() - t0, len(d)))
total = sum(c[1] for c in chunks)
print(f"recv events={len(chunks)} bytes={total} audio_s={total/32000:.1f}")
print("first data at %.3fs" % chunks[0][0] if chunks else "no data")
for t, n in chunks[:20]:
    print(f"  t={t:7.3f}s bytes={n}")
