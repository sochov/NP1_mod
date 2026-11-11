#!/bin/sh
# Read-only systemd inventory. Never starts/stops/reloads/enables any unit.
set -u
printf '=== service inventory ===\n'
systemctl list-unit-files --type=service --no-pager --no-legend
printf '\n=== loaded service inventory ===\n'
systemctl list-units --all --type=service --plain --no-pager --no-legend
printf '\n=== stock app template ===\n'
systemctl cat app-@.service
for unit in app-AppManager.service app-@System.service app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service app-@Navigation.service app-@Setting.service app-@Update.service app-@VirtualPassenger.service app-@InfoMtgEngine.service app-@SitAwrEng.service app-@Misc.service app-@RemoteMonitoring.service app-@Opening.service NEventWatcherCom.service NEventWatcherAV.service NEventWatcherITS.service NEventWatcherSys.service NPSystemMonitor.service qmmf-server.service QCMAP_ConnectionManagerd.service pulseaudio.service socketserver.service loc_launcher.service location_hal_daemon.service adbd.service dbus.service; do
 printf '\n=== UNIT %s ===\n' "$unit"
 systemctl show "$unit" -p Id -p LoadState -p ActiveState -p SubState -p UnitFileState -p MainPID -p Description -p FragmentPath -p DropInPaths -p Type -p User -p Group -p WorkingDirectory -p ExecStart -p ExecStop -p ExecStopPost -p Restart -p RestartUSec -p KillMode -p KillSignal -p SendSIGKILL -p TimeoutStartUSec -p TimeoutStopUSec -p StartLimitIntervalUSec -p StartLimitBurst -p RefuseManualStart -p RefuseManualStop -p CanStart -p CanStop -p CanReload -p StopWhenUnneeded -p Requires -p Wants -p Requisite -p BindsTo -p PartOf -p After -p Before -p Conflicts -p ConsistsOf -p BoundBy -p RequiredBy -p RequisiteOf -p Triggers -p TriggeredBy
 printf '\n--- unit definition ---\n'
 systemctl cat "$unit"
done
