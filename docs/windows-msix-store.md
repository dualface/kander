# Windows Microsoft Store (MSIX)

This page covers the Microsoft Store distribution of kander: the MSIX package, how it is built, how a Store install behaves, how to sideload and verify a test build, and what a Store submission needs. A successful build does not mean the package passed Store certification; the unverified items are listed at the end.

The approach follows the QuickTUI server Store package: an MSIX bundle for x64 and arm64, a full-trust desktop app, an execution alias as the CLI entry, an unsigned upload that the Store re-signs, and a separately marked sideload test build. Unlike QuickTUI, kander builds the bundle on a GitHub Actions Windows runner instead of a remote build VM.

## Terms

- **Store install**: a kander process with MSIX package identity, installed from Microsoft Store and started through the execution alias or the Start menu. `install.Packaged()` detects it with `GetCurrentPackageFullName`.
- **Package identity values**: the three public values on Partner Center > Product > Product management > Product identity: `Package/Identity/Name`, `Package/Identity/Publisher` (`CN=...`), and `Package/Properties/PublisherDisplayName`. They are not credentials.
- **Store upload bundle**: an unsigned `.msixbundle` built with the package identity values, for upload to Partner Center.
- **Sideload test bundle**: a `.msixbundle` built with the fixed test identity (`Kander.LocalTest`, `CN=Kander MSIX Local Test`) and signed with a throwaway self-signed certificate. Its name ends in `-localtest`; the Store rejects it.

## Package

- Manifest template: `packaging/msix/AppxManifest.xml.in`. A full-trust desktop app (`runFullTrust`), `TargetDeviceFamily Windows.Desktop` MinVersion `10.0.19041.0`, one package per architecture combined into one bundle.
- The package carries only `kander.exe` and the tile icons. It does not bundle or install any agent CLI or terminal backend.
- The execution alias `kander.exe` lives in `%LOCALAPPDATA%\Microsoft\WindowsApps\`, which Windows keeps on PATH. The package directory `C:\Program Files\WindowsApps\<full name>` is read-only and changes with every version.
- A Start menu launch opens the terminal kanban, like a bare `kander`.
- File and registry write virtualization is disabled (`desktop6:FileSystemWriteVirtualization` and `desktop6:RegistryWriteVirtualization` set to `disabled`, plus the `unvirtualizedResources` restricted capability). kander writes task files under `%TEMP%`, which is below `%LOCALAPPDATA%`, and hands them to agents that run outside the package (Claude, Codex, and others). With virtualization on, MSIX redirects AppData writes of a packaged process to a package-private location, so those agents might not find the files. Windows 10 2004 (19041) is the first release with these switches, which sets the minimum version. The Store asks for a justification of `unvirtualizedResources` at submission.
- Configuration, rules, and the board live below the user profile (`~/.config/kander`, `~/.agents/kander`, the project's `kanban/`), not in AppData, so they are shared with any non-Store install.

## Store Install Behavior

All of this applies only while `install.Packaged()` is true; every other install behaves as before.

- Updates belong to the Store. The bare-TUI startup check (`install.CheckUpdate`) returns no update without any network request, so no update dialog appears. `install.ApplyUpdate` refuses before any download or file write with `install.update_store_managed` ("kander was installed from Microsoft Store; updates are managed by Microsoft Store").
- No PATH copy. The bare-TUI startup copy question (`install.CheckStartupCopy`) is skipped, and `kander install` in global scope does not copy the binary and prints no PATH hint: the execution alias is already on PATH, and a copy outside the package would leave the Store's update channel.
- A project install still copies the binary into `<main worktree>/.kander/bin`. That copy has no package identity and behaves like any direct install, including self-updates.

## Build

`packaging/msix/build-msix.ps1` runs under PowerShell 7 on Windows with the Windows 10/11 SDK (`makeappx.exe`; `signtool.exe` for `-TestSign`):

```powershell
# Store upload bundle; values from Partner Center > Product identity
$env:KANDER_MSIX_IDENTITY_NAME = '...'
$env:KANDER_MSIX_PUBLISHER = 'CN=...'
$env:KANDER_MSIX_PUBLISHER_DISPLAY_NAME = '...'
pwsh packaging/msix/build-msix.ps1 -Version 0.9.15 -BinDir bin -OutDir out

# Sideload test bundle and its public certificate
pwsh packaging/msix/build-msix.ps1 -Version 0.9.15 -BinDir bin -OutDir out -TestIdentity -TestSign
```

- `-BinDir` holds `amd64\kander.exe` and `arm64\kander.exe`, built with `-ldflags "-X github.com/dualface/kander/internal/version.Version=<X.Y.Z>"`. `-Arches` narrows the architectures (default `amd64 arm64`).
- Without `-TestIdentity`, a missing package identity value fails the build; there are no defaults.
- Version mapping: kander `X.Y.Z` becomes MSIX `X.Y.Z.0`. The Store requires revision 0, every part at most 65535, and a version above the last published submission. Prerelease versions (with `-`) are rejected.
- The script lists the bundle entries and fails when an architecture is missing. With `-TestSign` it signs with a fresh self-signed certificate whose subject is the test publisher, exports the public `kander-msix-test.cer`, and deletes the certificate and its private key from the user store. Store uploads stay unsigned.
- The output directory also gets `SHA256SUMS`.
- Tile icons in `packaging/msix/Assets/` are exported from the logo `docs/images/kander-icon.svg`:

  ```sh
  rsvg-convert -w 44 -h 44 docs/images/kander-icon.svg -o packaging/msix/Assets/Square44x44Logo.png
  rsvg-convert -w 150 -h 150 docs/images/kander-icon.svg -o packaging/msix/Assets/Square150x150Logo.png
  rsvg-convert -w 50 -h 50 docs/images/kander-icon.svg -o packaging/msix/Assets/StoreLogo.png
  ```

### CI

`.github/workflows/msix.yml` runs the script on `windows-latest`:

| Trigger | Identity | Output |
| --- | --- | --- |
| push touching `packaging/msix/` or the workflow | test, signed | run artifact, version `0.0.1` |
| `workflow_dispatch` (ref, version, test_identity) | test or Store | run artifact |
| called by Release for a stable `vX.Y.Z` tag | Store | attached to that GitHub Release |

- The Store identity comes from the repository variables `KANDER_MSIX_IDENTITY_NAME`, `KANDER_MSIX_PUBLISHER`, and `KANDER_MSIX_PUBLISHER_DISPLAY_NAME` (Settings > Secrets and variables > Actions > Variables; they are public values, not secrets).
- When Release calls the workflow while a variable is unset, the job emits a notice and builds nothing; the release itself still succeeds. A manual Store build with unset variables fails.
- Release calls the workflow only for tags without `-`.
- GitHub offers `workflow_dispatch` only for workflow files on the default branch (`main`), so manual runs need a release that contains this file.

## Getting the First Store Upload Bundle

1. In Partner Center, create a product of type **MSIX or PWA app** (not "EXE or MSI app") and reserve the name Kander.
2. Copy the three package identity values from the product's identity page and store them as the repository variables above, for example `gh variable set KANDER_MSIX_IDENTITY_NAME --repo dualface/kander --body '...'`.
3. Release a new version through the normal release order in `AGENTS.md` "Releases" (this needs the `develop` to `main` fast-forward and the tag). The Release workflow attaches `kander_<X.Y.Z>.0.msixbundle` to the GitHub Release.
4. After a release that contains `msix.yml`, `workflow_dispatch` with `test_identity` off can rebuild the Store bundle for an existing tag as a run artifact, for example after the variables were set too late.
5. Upload the bundle in a new submission and fill in the submission materials below. The first submission is manual.

## Sideload Verification

Run on a full Windows 10 2004+ or Windows 11 machine in an interactive session (installing over SSH fails with 0x80070005). The Windows Firewall service must run, or installation fails with 0x80073D0A. Trust the test certificate in both stores on the test machine only and remove it afterwards:

```powershell
# Administrator PowerShell, test machine only
Import-Certificate -FilePath .\kander-msix-test.cer -CertStoreLocation Cert:\LocalMachine\TrustedPeople
Import-Certificate -FilePath .\kander-msix-test.cer -CertStoreLocation Cert:\LocalMachine\Root
Add-AppxPackage -Path .\kander_<version>-localtest.msixbundle
```

Record each result:

1. `kander version` through the alias prints the build version; `(Get-Process kander).Path` is under `WindowsApps`.
2. The Start menu entry opens the board in a terminal window.
3. Bare `kander` shows no PATH copy question and no update dialog, even when a newer GitHub release exists.
4. `kander install` (global) copies no binary and prints no PATH hint.
5. `kander start` on a test card: the agent outside the package reads the task file under `%TEMP%`.
6. `kander review` on a test card: the reviewer starts, finishes, and its processes are collected.
7. A herdr or other container launcher started from the packaged kander works and keeps running after kander exits.
8. Uninstall through PowerShell (`Remove-AppxPackage`) and through Settings > Apps; the alias disappears.
9. WACK: `appcert.exe test -appxpackagepath <bundle> -reportoutputpath report.xml`.

## Submission Materials

- Description: what kander does and that it drives AI coding agent CLIs the user installs separately (policy 10.2.4 dependency disclosure).
- Privacy policy URL (policy 10.5.1).
- Justifications for the restricted capabilities: `runFullTrust` (a desktop CLI that starts agent and terminal processes) and `unvirtualizedResources` (task files and settings must be visible to those processes outside the package). Each field takes at most 500 characters.
- Certification notes (policy 10.3.2): how a reviewer runs `kander` from a terminal and opens the board.
- Store logo: 1:1 box art 1080x1080 or 2160x2160 PNG, exported from the same SVG, for example `rsvg-convert -w 2160 -h 2160 docs/images/kander-icon.svg -o kander-store-2160.png`.
- Screenshots: PNG, at least 1366x768, up to 10, showing the board in a Windows terminal.

## Unverified Items and Risks

- Not verified by the build: interactive sideload installation and the execution alias; agents outside the package reading task files with write virtualization disabled; `kander review` inside the package, whose Windows launcher uses a job object and `CREATE_NEW_PROCESS_GROUP` (the QuickTUI package notes that process-group flags change console and shutdown behavior); terminal backends such as herdr started by a packaged process; uninstall paths; WACK; Smart App Control; Store certification.
- Updates pushed by the Store (rather than a sideload `Add-AppxPackage`) are untested.
- Child processes inherit package identity only when their executable is inside the package. Agent CLIs and terminal backends run outside it, as in a direct install.

## References (checked 2026-10-29)

- [MSIX package requirements and Store re-signing](https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/app-package-requirements)
- [Store policies](https://learn.microsoft.com/en-us/windows/apps/publish/store-policies)
- [Desktop extensions (execution alias)](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/desktop-to-uwp-extensions)
- [Behind the scenes of packaged desktop apps (file system and registry virtualization)](https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-behind-the-scenes)
- [Flexible virtualization and the desktop6 virtualization elements](https://learn.microsoft.com/en-us/windows/msix/desktop/flexible-virtualization)
- [Product identity](https://learn.microsoft.com/en-us/windows/apps/publish/view-app-identity-details)
- [Signing a package with a test certificate](https://learn.microsoft.com/en-us/windows/msix/package/create-certificate-package-signing)
- [Windows App Certification Kit](https://learn.microsoft.com/en-us/windows/uwp/debug-test-perf/windows-app-certification-kit)
