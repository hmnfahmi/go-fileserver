# go-fileserver

A lightweight self-hosted HTTP file server built with Go.

The project provides a simple web interface for browsing, uploading, downloading, searching, sorting, renaming, deleting, and previewing files from a configurable shared directory.

## Motivation

This application started as a personal utility for an environment where transferring files is sometimes inconvenient because of strict network and infrastructure policies.

Typical situations include:

- Transferring screenshots or photos from ATM-related activities when the relevant ATM screen cannot be accessed remotely.
- Moving files between machines when direct remote access is unavailable or restricted.
- Situations where FTP access is unavailable or disabled for certain users.
- Quickly sharing files over a local network without requiring a separate FTP server.

The goal is not to replace an organisation's official file-transfer or remote-access infrastructure. It is a lightweight utility for controlled, temporary file-transfer needs where HTTP access is available but other protocols may be restricted.

> **Important:** Use this application only in environments where you are authorised to run a local file server and transfer the relevant files. Do not use it to bypass organisational security controls or access restrictions.

## Features

### File management

- Browse files and directories
- Navigate nested directories
- Download files
- Upload files
- Multi-file upload
- Drag-and-drop upload
- Rename files and directories
- Delete files and directories
- Search files by name
- Sort by:
  - Name
  - Size
  - Modified time
- Ascending and descending sorting
- Breadcrumb navigation
- Human-readable file sizes
- Modified time display
- Text file preview

### Upload

The upload system supports:

- Single-file upload
- Multiple-file upload
- Drag-and-drop
- Duplicate filename detection
- Upload size limit
- Per-file success/conflict/failure reporting

### Configuration

Runtime configuration is provided through `config.yaml`.

The configuration model allows:

- Changing the HTTP server port without rebuilding the application.
- Selecting an external shared directory.
- Storing shared files on another drive.
- Configuring upload and preview size limits.
- Discovering the available network addresses at startup.

Example:

```yaml
server:
  port: "8088"

storage:
  shared_path: "./shared"
  max_upload_size: 104857600
  max_preview_size: 2097152
```

An external storage location can be used, for example:

```yaml
storage:
  shared_path: "D:/FileServerData/shared"
```

This allows a deployment such as:

```text
C:\
└── go-fileserver\
    ├── go-fileserver.exe
    └── config.yaml

D:\
└── FileServerData\
    └── shared\
        ├── files
        └── folders
```

The application binary and configuration can therefore remain on one drive while user data is stored somewhere else.

## Requirements

For running the released application:

- Windows or another supported Go runtime environment
- `go-fileserver.exe`
- `config.yaml`

For development:

- Go
- Git

The project intentionally uses Go's standard library where practical.

## Running

After building the application:

```powershell
.\go-fileserver.exe
```

The server listens on all interfaces (`0.0.0.0`) on the configured port, for example:

```text
HTTP File Server started
Config:      C:\go-fileserver\config.yaml
Port:        8088
Shared path: D:\FileServerData\shared

Available addresses:

  Wi-Fi
    http://192.168.1.10:8088

  Ethernet
    http://192.168.1.20:8088

  VMware Network Adapter VMnet1 (virtual adapter)
    http://192.168.56.1:8088

  Loopback Pseudo-Interface 1 (loopback, this machine only)
    http://127.0.0.1:8088
```

The application enumerates the machine's network interfaces and prints the usable IPv4
addresses, grouped by interface name. Loopback is labelled separately, and interfaces
whose names look like virtual adapters (VMware, VirtualBox, WSL, Docker, VPN/tunnel)
are marked as such. The application does not guess which address is the correct LAN
address; if several candidates exist, all of them are shown.

## Configuration

`config.yaml` is read from the directory containing the executable, or from the current
working directory if it is not found next to the executable. An explicit startup error
lists the locations that were searched when the file cannot be found. Configuration is
read once at startup: edit `config.yaml`, then restart the application.

Example `config.yaml`:

```yaml
server:
  port: "8088"

storage:
  shared_path: "./shared"
  max_upload_size: 104857600
  max_preview_size: 2097152
```

An invalid configuration (missing or malformed file, empty or out-of-range port,
non-positive size limits, empty shared path) stops the application with a message that
explains what is wrong.

### Server port

```yaml
server:
  port: "8088"
```

Both `"8088"` and `8088` are accepted. The value must be between `1` and `65535`.

### Shared path

A relative path:

```yaml
storage:
  shared_path: "./shared"
```

is resolved against the directory that contains `config.yaml` — not against an arbitrary
current working directory. This makes a portable deployment behave consistently no matter
how the application is launched.

An absolute path can also be used:

```yaml
storage:
  shared_path: "D:/FileServerData/shared"
```

Backslashes (`D:\FileServerData\shared`) are equally valid.

This is useful when the application is installed on one drive while file storage is
located on another drive.

The directory is created automatically if it does not exist.

### Upload size

```yaml
storage:
  max_upload_size: 104857600
```

The value is specified in bytes.

`104857600` = 100 MB.

### Preview size

```yaml
storage:
  max_preview_size: 2097152
```

The value is specified in bytes.

`2097152` = 2 MB.

The preview limit must not exceed `max_upload_size`.

## Architecture

The project follows a relatively small layered structure:

```text
Browser
   │
   ▼
HTTP Handler
   │
   ▼
Service
   │
   ▼
Filesystem
```

Current project structure:

```text
go-fileserver/
├── cmd/
│   └── go-fileserver/
│       └── main.go
├── internal/
│   ├── config/
│   ├── formatter/
│   ├── handler/
│   ├── model/
│   ├── netinfo/
│   └── service/
├── web/
│   ├── embed.go
│   ├── templates/
│   └── static/
├── config.yaml
├── go.mod
└── go.sum
```

### Main components

`cmd/go-fileserver`

Application entry point, HTTP route registration and startup output.

`internal/config`

Runtime configuration: file discovery, YAML loading, validation and path
resolution.

`internal/handler`

HTTP request handling.

`internal/service`

Filesystem and application logic.

`internal/model`

Data structures used by the application.

`internal/formatter`

Presentation helpers such as human-readable file sizes.

`internal/netinfo`

Network interface discovery and classification for the startup output.

`web`

Embedded HTML templates and static CSS/JavaScript assets. They are compiled
into the binary and are not required at runtime.

## Security Considerations

This application is designed as a lightweight file server, not as a replacement for a hardened enterprise file-transfer platform.

Current security considerations include:

- Path traversal protection
- Upload filename sanitisation
- Duplicate upload protection
- Upload size limit
- Preview size limit
- No authentication by default
- No authorisation layer
- Access is available to clients that can reach the configured server address/port
- Network exposure depends on the host firewall and network configuration

The application should therefore be run only on networks and machines where the operator understands who can reach the server.

Do not expose the server directly to the public Internet without an appropriate security design.

## Development

Clone the repository:

```bash
git clone <repository-url>
cd go-fileserver
```

Download dependencies:

```bash
go mod download
```

Run:

```bash
go run ./cmd/go-fileserver
```

Build:

```bash
go build ./...
```

Build a Windows executable (from any platform):

```bash
GOOS=windows GOARCH=amd64 go build -o go-fileserver.exe ./cmd/go-fileserver
```

Test:

```bash
go test ./...
```

Static analysis:

```bash
go vet ./...
```

Format:

```bash
gofmt -w .
```

## Deployment

The deployment is deliberately small:

```text
go-fileserver.exe
config.yaml
```

The web templates and CSS/JavaScript are embedded into the binary with Go's
`embed` package, so no `web/` directory is required at runtime. `config.yaml` is
never embedded and remains editable; the shared directory is created
automatically if it does not exist.

User data remains external and is controlled through `storage.shared_path`.

Example:

```text
Application
C:\go-fileserver\
├── go-fileserver.exe
└── config.yaml

User data
D:\FileServerData\
└── shared\
```

This separation makes it possible to update or replace the application without moving the shared files.

## Project Status

The core file-management functionality is implemented.

Current capabilities include:

- File and directory browsing
- Navigation
- Upload
- Multi-upload
- Drag-and-drop
- Download
- Search
- Sorting
- Rename
- Delete
- Breadcrumbs
- Text preview
- Runtime configuration
- Embedded web assets
- Network address discovery

Planned or potential future improvements include:

- Image preview
- ZIP/folder download
- File-type-specific icons
- Improved folder actions
- Further filesystem security hardening
- Structured logging
- Graceful shutdown
- Optional authentication/authorisation
- Health endpoint
- Pagination for very large directories

The roadmap is intentionally incremental. Features are added after the underlying filesystem and configuration behaviour are sufficiently reliable.

## Background

This project began as a practical personal tool rather than as a generic file-server implementation.

The initial problem was simple: sometimes a file needed to be transferred between machines, but the normal mechanisms available in an enterprise environment were not always suitable or available.

For example, an ATM-related task may require a screenshot or photo of a physical ATM screen. The screen itself may not be accessible through remote desktop or another remote-access mechanism, while the resulting image still needs to be transferred to another workstation. In other situations, FTP may be restricted or unavailable for particular users.

A small HTTP server was a useful alternative when the machines were already reachable over an allowed network path.

The project subsequently evolved from a small utility into a learning project for:

- Go HTTP servers
- Filesystem APIs
- HTTP handlers
- Configuration management
- File upload/download
- Path validation
- Security boundaries
- Testing
- Cross-drive storage
- Application packaging
- Lightweight deployment

## Disclaimer

This project is intended for authorised personal, development, laboratory, or operational use.

Always follow the security policies, network controls, data-handling requirements, and access rules of the environment in which the application is deployed.

Do not use the application to circumvent access controls, network restrictions, or organisational security policies.

## License

Add the appropriate license for this repository.
