# DWYT + Windows Terminal

[Windows Terminal](https://aka.ms/terminal) is the recommended terminal for
DWYT on Windows. It supports the UTF-8 box-drawing and emoji output DWYT uses,
plus tabs and PowerShell 7.

## Recommended setup

1. Install Windows Terminal and PowerShell 7:
   ```powershell
   winget install Microsoft.WindowsTerminal
   winget install Microsoft.PowerShell
   ```
2. Set **PowerShell** (pwsh) as the default profile in Terminal settings.
3. Ensure UTF-8 rendering (default in modern Terminal). If you see broken
   glyphs in an old console, run:
   ```powershell
   [Console]::OutputEncoding = [Text.Encoding]::UTF8
   ```

## A handy "DWYT" profile (optional)

Add a profile in Windows Terminal **Settings → Add a new profile** that opens
your projects folder and is ready for `dwyt .`:

```json
{
  "name": "DWYT",
  "commandline": "pwsh.exe -NoExit -Command \"cd $env:USERPROFILE\\code\"",
  "startingDirectory": "%USERPROFILE%\\code",
  "icon": "🦆"
}
```

Then in that tab:

```powershell
dwyt .
```

## Quake-mode quick access (optional)

Bind a global hotkey (Terminal **Settings → Actions**) to toggle the quake
window, so you can run `dwyt stop` / `dwyt status` from anywhere.
