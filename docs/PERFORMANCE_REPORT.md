# Harbor performance qualification

Date: 2026-10-21. Phase 2.

## Real Telegram measurements

Status: **PASS for controlled measurements and required Phase 2 acceptance scope**. Coordinator authenticated through native QR, verified the intended private channel and ran generated fixtures against real Telegram. Live browser upload/preview/verified download and retrieval after idle SIGKILL/restart also passed. Source for primary timings: unchanged sanitized `output/phase2/telegram-qualification.json`; figures below are measured, not estimates. Stress, sustained availability and in-flight failure benchmarks remain NOT TESTED.

One run per fixture, sequential HTTP operations, 4,194,304-byte application chunks. Upload API latency includes successful chunk requests and server finalization (which reads/decrypts all remote chunks to verify whole-file SHA-256). It excludes operator repair/restart pauses, failed finalization time and download time. These are end-to-end Harbor API rates, not isolated MTProto wire bandwidth.

| Fixture | Bytes | Chunks | Upload API seconds | Upload MiB/s | Download seconds | Download MiB/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| TXT | 68 | 1 | 0.276320 | 0.000234691 | 0.482156 | 0.000134500 |
| PNG | 121 | 1 | 0.246928 | 0.000467320 | 0.187252 | 0.000616252 |
| PDF | 612 | 1 | 0.298940 | 0.001952394 | 0.189365 | 0.003082131 |
| Multipart | 4,259,861 | 2 | 8.929332 | 0.454963485 | 3.656776 | 1.110956826 |

Each download passed byte equality and SHA-256 verification. The tiny fixtures are dominated by request latency; their rates are not useful large-file capacity estimates. Measured finalization time: TXT 0.120993 s, PNG 0.094153 s, PDF 0.157190 s, multipart 2.221936 s.

After coordinator restarted Harbor with the same data/session, every fixture passed checksum verification again:

| Fixture | Restart download seconds | Restart download MiB/s |
| --- | ---: | ---: |
| TXT | 0.092392 | 0.000701902 |
| PNG | 0.101655 | 0.001135157 |
| PDF | 0.076126 | 0.007666885 |
| Multipart | 4.589064 | 0.885261124 |

Server process sampling used `ps %cpu` and RSS nominally every 250 ms. CPU values are OS-reported samples, not per-file CPU accounting. RSS is a sampled maximum, **not a guaranteed process lifetime peak**.

| Phase | Wall seconds | Samples | Mean CPU % | Max sampled CPU % | Max sampled RSS bytes | Max sampled RSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Upload/stage | 7.919785 | 31 | 1.841935 | 14.4 | 41,648,128 | 39.71875 |
| Resume/finalize/download/Range | 10.044413 | 39 | 3.684615 | 10.5 | 47,218,688 | 45.03125 |
| Restart retrieval | 4.873898 | 19 | 4.826316 | 10.8 | 47,448,064 | 45.25000 |

Transfer concurrency: **1** runner HTTP operation, with the adapter's installed uploader limited to **2** MTProto part workers. Engine ceiling remains **4** transfer/read slots; download streaming is sequential. Recorded initial and final connection snapshots report `connected`, `retry_count=0`. This is not a cumulative retry count across restarted processes; whole-run retry history is **NOT TESTED**. No deliberate flood wait, real connection-loss experiment or stress benchmark was performed.

Initial TXT finalization exposed same-DC `DC_ID_INVALID`; coordinator retained its upload, corrected download routing and recovered it. State records `initial_verification_failure_recovered=true`. Failed verification and repair time are excluded from the successful API timings, and no uninterrupted cold-run claim is made. Phase wall totals span only their recorded measurement windows and must not be summed as whole-session duration.

Final functional follow-up: production UI uploaded/previewed/downloaded an additional 135,168-byte text fixture through real Telegram with byte and SHA-256 equality (`output/phase2/live-browser-checksums.json`). No browser throughput or CPU/RSS figure was collected, so those browser metrics remain **NOT TESTED**. After verified idle server SIGKILL/restart, all five fixtures remained intact; the separate multipart re-read took 3.569011 seconds at 1.138275983 MiB/s (`output/phase2/live-idle-crash.json`). This recovery read is separate from the primary benchmark and does not demonstrate an in-flight crash or connection-loss scenario. Read-only remote inspection verified all six tracked chunks were authenticated ciphertext (`output/phase2/live-ciphertext-probe.txt`).

## Reproducible measurement method

`scripts/telegram_qualification.py` records measurements to ignored, mode-0600 JSON under `output/`. Its `--live` mode requires authenticated diagnostics to report `adapter=telegram`. The controlled workload is three small generated fixtures (TXT, valid 16×16 RGB PNG, valid one-page PDF), followed by deterministic `multipart.bin` with one full application chunk plus 65,557 bytes. At default 4 MiB chunks the multipart file is 4,259,861 bytes and uses two application chunks. No personal files or bulk stress workload is used.

- Transfer concurrency: **1** runner HTTP operation; engine ceiling **4** simultaneous transfer/read slots. No concurrency tuning was introduced.
- Upload rate: fixture bytes divided by measured upload API time (begin + chunk PUTs + finalization). Finalization includes backend reads for authenticated whole-file validation. Restart/operator idle time is excluded from multipart upload timing.
- Download rate: complete fixture bytes divided by HTTP retrieval wall time, then SHA-256 and byte equality are checked.
- Per-file state: `upload_api_seconds`, `finalize_seconds`, `upload_mib_per_second`, `download.seconds`, `download.mib_per_second`, file size, chunk count and chunk size. Restart downloads are recorded separately.
- Per-phase elapsed time and sanitized `storage_connection` retry snapshots are recorded. Counters can reset after restart; compare within each process lifetime.
- Optional `--server-pid PID` samples `ps %cpu` and RSS every 250 ms. Results report OS-provided CPU percentage mean/max and **sampled peak RSS**, not a guaranteed lifetime high-water mark. Without a PID, process measurements are explicitly NOT TESTED. Container process measurements should be collected by coordinator using Docker when host PID does not identify server process.

Local runner validation: **PASS**, including all three phases, two actual process restarts, four checksum matches, cross-chunk Range and CPU/RSS sampler operation. Local-adapter timings are separate from the real Telegram measurements above.

Final local Docker qualification also passed all three phases and post-image-replacement retrieval. Its loopback timings are intentionally not presented as Telegram performance. Frontend displays acknowledged upload rate, remaining bytes and ETA; existing bytes from a resumed upload are excluded from its measured rate. Managed downloads report received-byte rate up to 32 MiB; larger downloads explicitly delegate progress to browser.

No concurrency increase or speculative performance tuning was introduced. The measured same-DC failure justified a narrow routing fix; upload completion intentionally pays for integrity validation. These small controlled measurements support functional qualification, not sustained service-level guarantees.
