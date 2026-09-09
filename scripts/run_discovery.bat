@echo off
REM Fired by the Windows scheduled task "NeuroFIQ-Discovery" every 2 minutes.
REM The slot (provider + city + role) comes from the clock inside the script,
REM so each run asks a different question without anything passed in here.
cd /d "%~dp0.."
python "scripts\discover_companies.py" --pages 5 --tick 120 >> "scripts\discovery_cron.log" 2>&1
