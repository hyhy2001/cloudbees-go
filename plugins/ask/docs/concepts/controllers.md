# Controllers — select the active Jenkins master

A **controller** is a managed Jenkins master on the CloudBees CI server. Commands for jobs, nodes, and credentials all run against one specific controller — you need to select one first.

## Select active controller

```bash
bee controller list             # see all available controllers
bee controller select <name>    # set the active controller
bee controller current          # confirm which is active
```

The selection is remembered **per profile** in the local DB — you only select once, not every session. Switching profiles also restores that profile's last-used controller.

## Show controller details

```bash
bee controller info <name>      # URL, online status, creation permissions
```

## Why "controller"?

A CloudBees CI Operations Center hosts one or more controllers (sometimes called "masters" or "instances"). Each controller has its own job list, node list, and credential store. `bee` must know which controller to target before it can list jobs, manage nodes, or access credentials.

If you run `bee job list` without selecting a controller, you get:

```
ERROR: No active controller selected. Run: bee controller select <name>
```
