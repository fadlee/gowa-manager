# Review Fix Tracking

Dokumen ini dipakai untuk tracking hasil review proyek dan checklist perbaikan. Fokus awal adalah risiko keamanan, proxy routing, stabilitas WebSocket, dan fondasi test.

## Current

Kondisi saat review terakhir:

- TypeScript typecheck sudah berjalan dan lulus lewat `bun run build:tsc`.
- `bun test` sudah berjalan dan lulus untuk test awal `basicAuth`.
- Route API utama (`/api/instances`, `/api/system`) sudah dilindungi Basic Auth.
- Keputusan desain: route proxy (`/app/<instanceKey>/...`) tidak memakai Basic Auth manager.
- Route proxy dipakai sebagai gateway sederhana untuk client eksternal/webhook yang mengakses API GOWA langsung.
- Proteksi proxy diserahkan ke auth per-instance GOWA, misalnya melalui config/flag auth GOWA pada masing-masing instance.
- WebSocket proxy otomatis menyuntikkan Basic Auth per-instance pertama ke upstream jika request tidak membawa `Authorization`, agar UI GOWA multi-device tetap berjalan di browser. Perilaku ini bisa dimatikan dengan `PROXY_WS_INJECT_INSTANCE_AUTH=false`.
- Konsekuensi: instance GOWA tanpa auth sendiri dapat diakses melalui proxy jika `instanceKey` diketahui.
- Route wildcard proxy didefinisikan sebelum route spesifik `status` dan `health`, sehingga berisiko menangkap request yang salah.
- Proxy meneruskan path lengkap dari manager ke instance tanpa path normalization yang jelas.
- Proxy mengembalikan `error.message` ke client pada response 502, sehingga detail internal seperti host/port dapat bocor.
- WebSocket proxy sekarang memakai connection id per client, bukan hanya `instanceKey`, sehingga client untuk instance yang sama tidak berbagi koneksi upstream.
- Password admin tidak lagi dicetak ke log saat server start.
- CORS sekarang memakai helper env-based: development tetap permissive, production deny-by-default kecuali `CORS_ALLOWED_ORIGINS` diisi.
- `basicAuth` dapat throw saat menerima invalid base64 karena `atob` tidak dibungkus `try/catch`, sehingga invalid Authorization header dapat menjadi 500, bukan 401.
- Perbandingan credential di `basicAuth` belum constant-time. Ini minor untuk konteks lokal, tetapi tetap perlu dicatat untuk hardening.
- Update config instance sekarang memaksa ulang `flags.basePath` seperti create instance.

## Next

Urutan perbaikan yang disarankan:

1. Dokumentasikan keputusan auth proxy: proxy memakai auth per-instance GOWA, bukan Basic Auth manager.
2. Tambahkan warning docs/UI bahwa instance GOWA perlu auth sendiri jika proxy diekspos ke jaringan publik.
3. Hardening `basicAuth`: invalid base64 harus 401, bukan 500; pertimbangkan constant-time comparison.
4. Sanitasi error proxy agar response 502 tidak membocorkan detail internal.
5. Perketat CORS untuk production atau buat konfigurasi allowed origins. (done: `CORS_ALLOWED_ORIGINS`)
6. Pindahkan route proxy spesifik (`status`, `health`, WebSocket) sebelum route wildcard.
7. Buat helper eksplisit untuk normalisasi proxy path sebelum request diteruskan ke GOWA.
8. Hentikan logging password admin di startup log.
9. Saat update instance, parse config dan paksa ulang `config.flags.basePath` berdasarkan instance key.
10. Ubah WebSocket proxy agar koneksi upstream dibuat per client connection, bukan per `instanceKey` global.
11. Bangun test suite komprehensif secara bertahap untuk backend, proxy, system/version, process utilities, frontend, dan e2e flow.
12. Tambahkan CI minimal untuk menjalankan `bun run build:tsc`, `bun test`, dan build frontend/production saat test suite sudah stabil.

## Proxy Auth Decision

Keputusan: proxy `/app/<instanceKey>/...` tidak menggunakan Basic Auth manager.

Alasan:

- Proxy dipakai sebagai gateway sederhana untuk client eksternal/webhook yang mengakses API GOWA.
- Client eksternal tidak perlu mengetahui credential admin manager.
- Auth dan authorization untuk API GOWA dikelola oleh masing-masing instance GOWA.

Konsekuensi:

- Instance GOWA yang tidak mengaktifkan auth sendiri dapat diakses melalui proxy jika `instanceKey` diketahui.
- Jika WebSocket auth injection aktif, siapa pun yang bisa mengakses URL proxy WebSocket untuk instance tersebut dapat memakai credential per-instance yang disuntikkan oleh manager ke upstream.
- Deployment yang expose proxy ke jaringan publik harus mengaktifkan auth per-instance GOWA.
- Perbaikan keamanan proxy tetap wajib: sanitasi error, path normalization, route order, dan tidak membocorkan detail internal.
- Untuk deployment publik yang tidak menginginkan WebSocket auth injection, set `PROXY_WS_INJECT_INSTANCE_AUTH=false` dan pastikan client eksternal mengirim auth GOWA sendiri.

## Checklist

### Security

- [x] Model auth proxy diputuskan: proxy memakai auth per-instance GOWA, bukan Basic Auth manager.
- [x] Keputusan auth proxy terdokumentasi di docs pengguna, termasuk dampaknya ke client eksternal/webhook.
- [x] UI atau docs memberi warning bahwa proteksi proxy bergantung pada config GOWA instance seperti `flags.basicAuth`.
- [x] WebSocket proxy menyuntikkan Basic Auth per-instance pertama ke upstream secara default jika request tidak membawa `Authorization`.
- [x] WebSocket auth injection bisa dimatikan dengan `PROXY_WS_INJECT_INSTANCE_AUTH=false`.
- [x] Endpoint proxy `status` dan `health` mengikuti desain auth proxy yang sudah dipilih.
- [x] `basicAuth` manager tetap hanya melindungi API manager seperti `/api/instances` dan `/api/system` (verified by route integration test).
- [x] `basicAuth` mengembalikan 401 untuk Authorization header invalid, termasuk invalid base64.
- [x] `basicAuth` memakai perbandingan credential timing-safe untuk username/password.
- [x] Startup log tidak mencetak password admin.
- [x] Error response proxy 502 tidak mengembalikan `error.message` mentah.
- [x] Error response proxy tidak membocorkan detail upstream ke response client pada test route integration.
- [x] Error response global tidak membocorkan stack trace, credential, host internal, atau port internal pada response helper test.
- [x] CORS production tidak memakai kombinasi open origin dan credentials tanpa allowlist.
- [x] CORS allowlist bisa dikonfigurasi lewat `CORS_ALLOWED_ORIGINS`.

### Proxy Routing

- [x] Route `/:instanceKey/status` didefinisikan sebelum wildcard `/:instanceKey/*`.
- [x] Route `/:instanceKey/health` didefinisikan sebelum wildcard `/:instanceKey/*`.
- [x] Route WebSocket didefinisikan sebelum wildcard HTTP jika diperlukan oleh router.
- [x] Wildcard hanya menangkap request proxy umum setelah route spesifik gagal match.
- [x] Variabel unused seperti `proxyPath` dihapus atau dipakai sesuai tujuan.
- [ ] Variabel unused lain seperti `instanceKey` di loop WebSocket cleanup dihapus atau dipakai.

### Proxy Path Handling

- [x] Ada helper/function yang jelas untuk mengubah path manager menjadi path target GOWA.
- [x] Path `/app/<key>/devices` diteruskan ke path target yang benar dan sudah dites.
- [x] Query string tetap dipertahankan saat request diteruskan.
- [ ] Binary response tetap pass-through tanpa transformasi rusak.
- [ ] JSON response URL rewrite punya perilaku yang eksplisit dan terdokumentasi.

### Instance Config

- [x] Update instance mem-parse JSON config dengan error handling yang jelas.
- [ ] Update instance melakukan merge dengan default config bila diperlukan.
- [x] `config.flags.basePath` selalu dipaksa menjadi `/${Proxy.PREFIX}/${existing.key}`.
- [x] Service-level test memastikan `updateInstance` menyimpan basePath yang dipaksa dan mempertahankan field existing saat omitted.
- [x] Config invalid tidak diam-diam disimpan sebagai string rusak.
- [ ] Perubahan version/name/config tetap backward compatible dengan UI.

### WebSocket

- [x] Koneksi upstream WebSocket tidak disimpan hanya berdasarkan `instanceKey`.
- [x] Setiap client connection punya upstream connection sendiri atau connection id unik.
- [x] Menutup satu browser tab tidak menutup koneksi client lain untuk instance yang sama.
- [x] Message forwarding memakai helper agar string/binary payload tidak selalu dipaksa `JSON.stringify`.
- [ ] Cleanup connection berjalan saat client close, upstream close, dan error (registry covered, route/upstream integration pending).

### Testing Roadmap

Tujuan: membangun test suite komprehensif secara bertahap, bukan hanya test untuk temuan review. Setiap fase harus menjaga `bun test` dan `bun run build:tsc` tetap hijau.

#### Phase 0 - Harness And Smoke Tests

- [x] `bun test` punya minimal satu test file dan tidak gagal karena `No tests found`.
- [x] `test/setup.ts` tersedia sesuai preload di `bunfig.toml`.
- [x] Test awal `basicAuth` tersedia untuk no header, invalid base64, auth type salah, credential salah, credential benar.
- [ ] Struktur folder test disepakati: colocated `*.test.ts` atau folder `tests/` per domain.
- [ ] Dokumentasi command test `bun run test` atau `bun test` ditambahkan ke README atau docs development.

#### Phase 1 - Backend Unit Tests

- [x] Test `basicAuth` tambahan untuk credential tanpa `:`, password mengandung `:`, casing auth scheme, dan malformed header.
- [ ] Test CLI config parsing untuk env default, CLI override, port validation, username/password validation, dan data dir.
- [x] Test `ConfigParser`: default config, parse JSON valid/invalid, env var generation, CLI arg generation, dan flag edge cases.
- [x] Test `NameGenerator` untuk format nama dan batas nilai random.
- [x] Test `DirectoryManager` dengan temporary data dir agar create/cleanup aman.
- [x] Test DB helper ringan seperti `generateInstanceKey` untuk panjang dan character set.

#### Phase 2 - Instance Service Tests

- [x] Test create instance: auto port, generated key, generated basePath, default config, selected `gowa_version`.
- [x] Test update instance: name/version/config update, invalid config handling, dan `basePath` tetap dipaksa sesuai key.
- [x] Test helper update config memastikan `basePath` tetap dipaksa sesuai key dan invalid JSON tidak disimpan mentah.
- [x] Test delete instance: stop process jika running, cleanup directory, clear resource history, delete DB row.
- [x] Test start instance success path dengan mock `VersionManager`, `SystemService`, `Bun.spawn`, dan `ProcessManager`.
- [x] Test start instance failure path: version tidak tersedia, port unavailable, spawn gagal, status menjadi `error` dengan message aman.
- [x] Test stop/kill/restart lifecycle tanpa menjalankan binary GOWA asli.
- [x] Test get status untuk running/stopped/error termasuk resource usage fallback saat monitor gagal.

#### Phase 3 - Proxy Tests

- [x] Test proxy auth decision: manager auth tidak diwajibkan untuk `/app/<key>/...`, sesuai keputusan opsi 2.
- [x] Test route order proxy untuk `status`, WebSocket, dan wildcard HTTP.
- [x] Test proxy path normalization dengan query string.
- [x] Test proxy error sanitization agar detail internal seperti host/port target tidak muncul di response.
- [x] Test header forwarding: auth/cookie preservation, host rewrite, dan forwarded headers.
- [x] Test body forwarding untuk JSON object dan text body.
- [x] Test binary response pass-through tanpa transformasi.
- [x] Test JSON URL rewrite behavior sesuai keputusan desain.
- [x] Test proxy status dan health untuk instance missing, stopped, running, dan upstream timeout.

#### Phase 4 - WebSocket Tests

- [x] Test WebSocket connection dibuat per client connection atau connection id unik, bukan hanya per `instanceKey`.
- [x] Test multiple client untuk instance yang sama tidak saling menutup koneksi.
- [ ] Test cleanup saat client close, upstream close, dan error (registry covered, route/upstream integration pending).
- [x] Test message forwarding mempertahankan tipe payload yang benar.
- [x] Test forwarding query string dan auth injection helper untuk WebSocket.
- [x] Test forwarding header penting lain seperti cookie/subprotocol.

#### Phase 5 - System And Version Tests

- [ ] Test `SystemService` port availability dan next available port dengan mock socket/server.
- [ ] Test system status response shape dan resource fallback.
- [x] Test `VersionManager` path resolution untuk `latest` dan explicit version.
- [x] Test installed versions listing dengan temporary data dir.
- [ ] Test install/remove version dengan mocked GitHub fetch dan filesystem temp dir.
- [ ] Test auto-updater behavior dengan mocked available versions dan installed versions.
- [ ] Test cleanup scheduler behavior tanpa menghapus data nyata.

#### Phase 6 - Route/API Integration Tests

- [ ] Test `/api/health` public access.
- [x] Test protected manager route membutuhkan Basic Auth manager melalui route integration harness.
- [x] Test protected `/api/instances` langsung membutuhkan Basic Auth manager.
- [x] Test protected `/api/system` langsung membutuhkan Basic Auth manager.
- [x] Global Bun test setup memakai isolated `.test-data/bun-<pid>` sebagai `DATA_DIR` dan cleanup saat process exit.
- [x] Test setup memfilter log noisy yang sudah dikenal dari DB init/migration, directory cleanup, dan lifecycle start mock.
- [x] Test CRUD instance routes dengan test database/temp data dir.
- [x] Test action routes start/stop/restart/kill dengan mocked process layer.
- [x] Test system status/config route shape dasar dengan isolated test data dir.
- [x] Test auth login/logout response shape.
- [x] Test CORS config untuk dev default, production deny-by-default, dan `CORS_ALLOWED_ORIGINS` allowlist.
- [x] Test global error handler untuk validation, unauthorized, not found, dan generic error.
- [ ] Test CORS behavior untuk development dan production config.

#### Phase 7 - Frontend Unit/Component Tests

- [ ] Pilih frontend test stack: Bun DOM support, Vitest, atau React Testing Library.
- [ ] Test `apiClient` request success/error handling dan Authorization integration.
- [ ] Test `AuthProvider`: login success/failure, stored credentials, logout.
- [ ] Test `LoginPage` form behavior dan error display.
- [ ] Test `DashboardPage` loading/error/empty/success states dengan mocked query client.
- [ ] Test `InstanceCard` action buttons untuk running/stopped/error states.
- [ ] Test create/edit instance dialog validation dan submit payload.
- [ ] Test `VersionSelector` installed/available/install states.

#### Phase 8 - End-To-End Tests

- [ ] Pilih e2e runner, misalnya Playwright, jika dependency tambahan disetujui.
- [ ] Test login manager dan membuka dashboard.
- [ ] Test create instance dengan mocked atau fake GOWA binary.
- [ ] Test start/stop/restart lifecycle end-to-end.
- [ ] Test proxy access ke fake upstream GOWA.
- [ ] Test external client scenario untuk `/app/<key>/...` tanpa manager auth tetapi dengan auth per-instance.
- [ ] Test responsive UI smoke untuk desktop dan mobile viewport.

#### Phase 9 - CI And Coverage

- [ ] CI menjalankan `bun install`.
- [ ] CI menjalankan `bun run build:tsc`.
- [ ] CI menjalankan `bun test`.
- [ ] CI menjalankan frontend build `bun run build:client` atau production build jika durasi masih wajar.
- [ ] Coverage threshold ditentukan setelah test baseline stabil.
- [ ] Test yang butuh network, GitHub API, atau binary GOWA asli dipisahkan dari default CI.
- [ ] Flaky test policy dibuat: tidak ada real timer panjang, real network, atau dependency port tetap.

### Documentation And CI

- [x] README atau docs menjelaskan bahwa proxy route memakai auth per-instance GOWA, bukan Basic Auth manager.
- [ ] Docs proxy path/basePath diperbarui jika perilaku path diubah.
- [x] README menjelaskan `CORS_ALLOWED_ORIGINS` dan `PROXY_WS_INJECT_INSTANCE_AUTH`.
- [x] Docs development menambahkan command test yang diharapkan.
- [x] `.test-data/` di-ignore agar isolated test database tidak masuk git.
- [ ] GitHub Actions atau CI lain menjalankan `bun run build:tsc`.
- [ ] GitHub Actions atau CI lain menjalankan `bun test`.

## Definition Of Done

Perbaikan dianggap selesai jika:

- Keputusan desain auth proxy sudah dibuat: proxy memakai auth per-instance GOWA, bukan Basic Auth manager.
- Keputusan desain auth proxy terdokumentasi untuk pengguna.
- Semua checklist Security dan Proxy Routing selesai.
- `bun run build:tsc` lulus.
- `bun test` lulus dengan minimal test untuk area yang diperbaiki.
- Perubahan perilaku proxy dan auth terdokumentasi.
- Tidak ada credential sensitif yang tercetak di log normal.
