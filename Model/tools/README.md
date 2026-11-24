# Generator of the BT and MON state machines

`specs.js` describes the state machines of Band Transport (1) and the monitor (2) as data; `smgen.js` writes them into
`BT.uml`/`BT.notation` and `MON.uml`/`MON.notation` in the structure Papyrus itself writes. Run `node specs.js BT`
and `node specs.js MON` from any folder (it changes into `Model/PapyrusProject`) on files **without** these diagrams
(`git checkout` them first). The diagrams can be edited in Papyrus afterwards like any other; the generator is only
how they were first created on 24 Nov 2026 (see `src/docs/diagram-check.md`).
