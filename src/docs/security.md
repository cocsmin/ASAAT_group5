# Security: out of scope (decision of 24 Nov)

Security is not a stated quality attribute of the Cake Factory, so the implementation does not
try to be secure. The report names this as a limit. What is open in the demo setup, and what a
plant would add:

| In the demo | Risk | A plant would add |
|---|---|---|
| The MQTT broker accepts anonymous clients, no TLS | anyone on the network can read every signal and publish a fake birth, heartbeat, death certificate or command (including "Section command" to Band Transport) | a username and password or client certificate per system, TLS, and access rules per topic (a system writes only its own topics) |
| The SQL Server password is a demo default in `docker-compose.yml` | the database is open to anyone who reads the repository | secrets outside the compose file (Docker secrets or a vault), a database user per system with the rights it needs |
| MongoDB has no authentication | pan and cake documents can be changed | authentication and a role per station |
| The dashboard buttons play Maintenance (*Mark repaired*), no login, no role | anyone who can open the page can confirm a repair | login, a Maintenance role, an audit of who confirmed what |
| The `ctl` container (demo profile) controls every Docker container through the Docker socket | whoever reaches it controls the machine | not deployed in a plant; the demo profile exists only for the demonstrations |
| Fault injection (`/api/inject`) is open | anyone can trigger a failure case | removed outside the simulation |

Nothing here changes the architecture: Sparkplug B and MQTT support authentication and TLS, and
the systems are separate containers, so the measures above are configuration, not redesign.
