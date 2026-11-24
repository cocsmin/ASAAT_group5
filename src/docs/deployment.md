# Deployment view

One image holds all stations; Docker Compose starts one container per station, plus the broker, the two
databases, the dashboard and the optional control service. This is the view for the report (a Papyrus deployment
diagram would show the same nodes).

```mermaid
flowchart LR
  subgraph host[Docker host]
    subgraph infra[infrastructure]
      MQ[(mosquitto<br/>MQTT broker :1883)]
      SQL[(mssql<br/>SQL Server)]
      MG[(mongo<br/>MongoDB)]
    end
    subgraph plm[PLM]
      plmwh[plm-wh]
      plmship[plm-ship]
    end
    subgraph bat[BAT]
      dose[bat-dose]
      mix[bat-mix]
      insp[bat-insp]
    end
    subgraph thm[THM]
      dep[thm-dep]
      bake[thm-bake]
      exit[thm-exit]
      cool[thm-cool]
    end
    subgraph fin[FIN]
      dpn[fin-dpn]
      qua[fin-qua]
      wrap[fin-wrap]
      pal[fin-pal]
      wash[fin-wash]
    end
    bt[bt<br/>band transport]
    mon[mon<br/>monitor]
    ui[ui<br/>dashboard :8080]
    ctl[ctl<br/>control, demo profile]
  end
  plm & bat & thm & fin & bt & mon & ui --- MQ
  plmwh & plmship & mon --- SQL
  plm & bat & thm & fin & bt --- MG
  ctl -. Docker socket .-> host
```

| Node | Image | State on a volume | Talks to |
|---|---|---|---|
| 14 stations, `bt`, `mon` | one image (`cmd/station`, `cmd/bt`, `cmd/mon`), the node is chosen by an environment variable | outbox and saved queues (`*-data`) | broker; SQL (PLM, MON); MongoDB (pan and cake documents, saved queues) |
| `mosquitto` | eclipse-mosquitto | persistent sessions | every node |
| `mssql` | SQL Server 2022 | `mssql-data` | PLM, MON |
| `mongo` | MongoDB | `mongo-data` | stations |
| `ui` | `cmd/ui` | none | broker, MON API, SQL |
| `ctl` | `cmd/ctl` (profile `demo`) | none | Docker socket |

Start: `docker compose up -d --build` (`docker compose --profile demo up -d --build` adds `ctl`). A container that
exits is restarted by Docker (`restart: unless-stopped`); after the restart it is reborn on the broker and takes up
its pans and cakes from the stored documents ([stations.md](stations.md), [containers.md](containers.md)). CI
builds and pushes the images to ghcr.io from `master`; there is no deploy job because there is no target machine
(decision 24 Nov: the compose file is the deployment).
