# TUI issues — garbled display, immediate exit, ASCII mode

## TUI shows garbled boxes or `?` characters — Unicode not supported

Your terminal doesn't support Unicode box-drawing characters. Force ASCII mode:

```bash
BEE_ASCII=1 bee --ui
```

Or set it permanently in your shell profile:

```bash
export BEE_ASCII=1
```

## TUI launches but immediately exits — not a TTY

`bee --ui` requires an interactive terminal (TTY). You cannot pipe it or run it in a non-interactive script. Run it directly in your terminal emulator.

## `bee --install` says "wrapper created" but `bee` isn't found

`~/.local/bin` may not be on your `PATH`. Add it:

```bash
export PATH="$HOME/.local/bin:$PATH"   # add to ~/.bashrc or ~/.zshrc
```

## Still stuck?

Run the failing command with `--debug` to get the full stack trace:

```bash
bee --debug <failing command> 2>&1
```

Or set the env var permanently:

```bash
BEE_DEBUG_TRACEBACK=1 bee <failing command>
```
