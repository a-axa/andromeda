# PRD: Andromeda

*Cross-platform network monitor — Go*

## 1. Overview

Andromeda adalah network monitoring tool cross-platform (Linux, Windows, macOS) berbasis Go. Tujuannya membantu user mengidentifikasi interface/koneksi jaringan dengan performa terbaik berdasarkan latency dan packet loss, lalu menampilkannya secara live di terminal.

Arsitektur dirancang modular dan scalable sejak awal supaya fitur-fitur lanjutan (WiFi scan, combo/load-balancing, dll) bisa ditambahkan tanpa refactor besar.

## 2. Problem Statement

User sering punya beberapa pilihan jaringan (WiFi, ethernet, tethering) tapi tidak tahu mana yang paling stabil tanpa trial-and-error manual satu-satu.

## 3. Goals — Scope v1 (Core)

- Deteksi semua network interface yang tersedia di sistem
- Ukur latency dan packet loss tiap interface secara paralel (goroutine per interface)
- Hitung skor ranking gabungan dari metrik yang didapat
- Tampilkan hasil secara live di terminal UI (TUI), auto-update berkala

## 4. Non-Goals v1 (Rencana Lanjutan)

Tidak masuk scope v1, tapi arsitektur harus mendukung penambahannya tanpa refactor besar:

- **WiFi SSID scan** — list SSID sekitar + signal strength (OS-specific: `iwlist`/netlink di Linux, `wlanapi` di Windows)
- **Combo Mode (load balancing)** — distribusi traffic ke multiple interface sekaligus (round-robin / least-latency routing)
- **Speed test** — throughput download/upload ke server eksternal
- **Persistence/history** — simpan histori skor untuk analisa tren dari waktu ke waktu

## 5. Target Platform

Cross-platform: Linux, Windows, macOS.

- Core logic (interface detection umum, ping, scoring, TUI) ditulis pure Go, tidak butuh wrapper OS-specific
- Fitur yang butuh akses OS-specific (WiFi scan, signal strength) diisolasi lewat build tags (`//go:build linux`, `//go:build windows`) di belakang interface abstraction yang sama

## 6. Architecture Principles

- **Modular per fitur** — tiap modul punya subfolder sendiri, tidak numpuk di satu file besar
- **Testable by default** — tiap modul membawa unit test-nya sendiri di subfolder yang sama
- **Clean boundaries** — modul saling berkomunikasi lewat interface Go yang jelas, bukan saling import internal detail
- **OS-specific terisolasi** — logic yang platform-dependent tidak bocor ke modul core

## 7. Folder Structure

```
andromeda/
├── main.go
├── go.mod
├── internal/
│   ├── iface/              # interface detection
│   │   ├── iface.go
│   │   └── iface_test.go
│   ├── probe/              # ping + latency measurement
│   │   ├── probe.go
│   │   └── probe_test.go
│   ├── score/              # ranking/scoring logic
│   │   ├── score.go
│   │   └── score_test.go
│   └── wifi/                # (future) WiFi SSID scan, OS-specific
│       ├── wifi_linux.go
│       ├── wifi_windows.go
│       └── wifi_test.go
├── ui/
│   ├── tui.go               # bubbletea terminal UI
│   └── tui_test.go
└── docs/
    └── PRD.md
```

Setiap modul baru (combo mode, speed test, dst) mengikuti pola yang sama: satu subfolder, satu file implementasi, satu file test.

## 8. Core Modules — v1

### 8.1 Interface Detection (`internal/iface`)

- List semua network interface aktif di sistem
- Ambil metadata dasar: nama, IP, status up/down

### 8.2 Probe / Latency Measurement (`internal/probe`)

- Ping target default (mis. 8.8.8.8 / 1.1.1.1) lewat tiap interface secara paralel
- Ukur latency (ms) dan packet loss (%) per interface

### 8.3 Scoring & Ranking (`internal/score`)

- Hitung skor gabungan dari latency + packet loss
- Urutkan interface dari performa terbaik ke terburuk

### 8.4 Terminal UI (`ui`)

- Tampilkan ranking secara live, update berkala
- Dibangun dengan `bubbletea` + `lipgloss`

## 9. Tech Stack

- **Go** — core logic, cross-platform
- **bubbletea + lipgloss** — terminal UI
- **Pure Go ICMP/ping** — hindari dependency CLI eksternal; perhatikan permission raw socket per OS

## 10. Testing Strategy

- Setiap modul (`iface`, `probe`, `score`, `ui`) punya `_test.go` di subfolder yang sama
- Unit test fokus ke logic murni (scoring formula, parsing output) — I/O jaringan di-mock, bukan dites langsung ke network asli

## 11. Success Criteria — v1

- Jalan tanpa modifikasi kode di Linux dan Windows
- Ranking interface akurat dan update real-time di TUI
- Codebase modular dan siap ditambah fitur WiFi scan + combo mode tanpa refactor besar
