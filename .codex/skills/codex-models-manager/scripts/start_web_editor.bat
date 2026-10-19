@echo off
setlocal
set SCRIPT_DIR=%~dp0
echo [Codex Model Catalog] Starting Visual Web Editor...
python "%SCRIPT_DIR%web_editor.py" %*
pause
