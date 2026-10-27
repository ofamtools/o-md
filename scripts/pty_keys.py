#!/usr/bin/env python3
"""Run a command in a pseudo-terminal and type keys into it.

Usage: pty_keys.py KEYS -- COMMAND [ARGS...]

KEYS is a JSON list: a string is typed as is, {"key": NAME} sends a named
key (esc, enter, tab, ctrl+c), {"sleep": SECONDS} waits. The terminal is
100x30. The command's output is drained and discarded; the exit status is
the command's.
"""
import fcntl, json, os, pty, select, struct, sys, termios, time

NAMED = {"esc": "\x1b", "enter": "\r", "tab": "\t", "backspace": "\x7f", "ctrl+c": "\x03"}

def drain(fd, seconds):
    end = time.time() + seconds
    while True:
        left = end - time.time()
        if left <= 0:
            return
        r, _, _ = select.select([fd], [], [], left)
        if not r:
            return
        try:
            if not os.read(fd, 65536):
                return
        except OSError:
            return

def main():
    sep = sys.argv.index("--")
    keys = json.loads(sys.argv[1])
    cmd = sys.argv[sep + 1:]
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.execvp(cmd[0], cmd)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
    drain(fd, 1.5)
    for k in keys:
        if isinstance(k, dict) and "sleep" in k:
            drain(fd, k["sleep"])
            continue
        text = NAMED[k["key"]] if isinstance(k, dict) else k
        for ch in text:
            os.write(fd, ch.encode())
            drain(fd, 0.05)
        drain(fd, 0.3)
    deadline = time.time() + 5
    while time.time() < deadline:
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            sys.exit(os.waitstatus_to_exitcode(status))
        drain(fd, 0.2)
    os.kill(pid, 9)
    print("pty_keys: the command did not exit", file=sys.stderr)
    sys.exit(1)

main()
