# Packs kander.exe builds into one Microsoft Store .msixbundle.
# Runs under PowerShell 7 on Windows with the Windows 10/11 SDK (makeappx.exe,
# and signtool.exe for -TestSign). The Release workflow runs it on a GitHub
# Actions Windows runner; see docs/windows-msix-store.md.
#
#   pwsh packaging/msix/build-msix.ps1 -Version 0.9.15 -BinDir dist -OutDir out
#
# BinDir holds <arch>\kander.exe for every architecture in -Arches.
#
# The Store identity comes from Partner Center > Product identity through
# KANDER_MSIX_IDENTITY_NAME, KANDER_MSIX_PUBLISHER and
# KANDER_MSIX_PUBLISHER_DISPLAY_NAME and is never defaulted. Store uploads stay
# unsigned: the Store signs them. -TestIdentity uses a fixed, clearly marked
# identity whose bundle name ends in -localtest and cannot be uploaded.
# -TestSign signs the bundle with a throwaway self-signed certificate for
# sideload testing; its private key is deleted right after signing and only
# the public .cer is written.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$BinDir,
    [Parameter(Mandatory = $true)][string]$OutDir,
    [string]$Arches = 'amd64 arm64',
    [switch]$TestIdentity,
    [switch]$TestSign
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Find-SdkTool([string]$Name) {
    $kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $tool = Get-ChildItem -LiteralPath $kits -Directory -Filter '10.*' -ErrorAction SilentlyContinue |
        Sort-Object { [version]$_.Name } -Descending |
        ForEach-Object { Join-Path $_.FullName "x64\$Name" } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Select-Object -First 1
    if (-not $tool) { throw "$Name not found under $kits; install the Windows 10/11 SDK" }
    return $tool
}

function Invoke-Tool([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$(Split-Path -Leaf $Tool) failed with exit code $LASTEXITCODE" }
}

# kander X.Y.Z -> MSIX X.Y.Z.0: the Store requires revision 0, every part
# <= 65535, and a version above the last published submission.
if ($Version -notmatch '^(\d{1,5})\.(\d{1,5})\.(\d{1,5})$') {
    throw "version must be a stable X.Y.Z release, got '$Version'"
}
foreach ($part in $Matches[1..3]) {
    if ([int]$part -gt 65535) { throw "version part exceeds 65535: $Version" }
}
$packageVersion = "$Version.0"

if ($TestIdentity) {
    $identityName = 'Kander.LocalTest'
    $publisher = 'CN=Kander MSIX Local Test'
    $publisherDisplayName = 'Kander Local Test'
    $suffix = '-localtest'
} else {
    $identityName = $env:KANDER_MSIX_IDENTITY_NAME
    $publisher = $env:KANDER_MSIX_PUBLISHER
    $publisherDisplayName = $env:KANDER_MSIX_PUBLISHER_DISPLAY_NAME
    foreach ($pair in @(
            @('KANDER_MSIX_IDENTITY_NAME', $identityName),
            @('KANDER_MSIX_PUBLISHER', $publisher),
            @('KANDER_MSIX_PUBLISHER_DISPLAY_NAME', $publisherDisplayName))) {
        if ([string]::IsNullOrWhiteSpace($pair[1])) {
            throw "$($pair[0]) is required (Partner Center > Product identity); pass -TestIdentity for a sideload test build"
        }
    }
    $suffix = ''
}
if ($identityName -notmatch '^[A-Za-z0-9.-]{3,50}$') { throw "invalid package identity name: $identityName" }
if (-not $publisher.StartsWith('CN=')) { throw "publisher must be a distinguished name starting with CN=: $publisher" }

$archList = @($Arches -split '\s+' | Where-Object { $_ } | Select-Object -Unique)
if ($archList.Count -eq 0) { throw 'no architectures selected' }
$msixArch = @{ amd64 = 'x64'; arm64 = 'arm64' }
foreach ($arch in $archList) {
    if (-not $msixArch.ContainsKey($arch)) { throw "unsupported architecture: $arch" }
}

$packagingDir = $PSScriptRoot
$template = Get-Content -Raw -Encoding utf8 -LiteralPath (Join-Path $packagingDir 'AppxManifest.xml.in')
$makeAppx = Find-SdkTool 'makeappx.exe'

$OutDir = [System.IO.Path]::GetFullPath($OutDir)
$work = Join-Path ([System.IO.Path]::GetTempPath()) "kander-msix-$([guid]::NewGuid().ToString('N'))"
$bundleInput = Join-Path $work 'bundle-input'
New-Item -ItemType Directory -Force -Path $OutDir, $bundleInput | Out-Null
$bundleName = "kander_$packageVersion$suffix.msixbundle"
$bundle = Join-Path $OutDir $bundleName
# Never leave an older bundle of the same name behind a failed build.
Remove-Item -LiteralPath $bundle, (Join-Path $OutDir 'kander-msix-test.cer') -ErrorAction SilentlyContinue

try {
    foreach ($arch in $archList) {
        $exe = Join-Path $BinDir "$arch\kander.exe"
        if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { throw "missing $exe" }
        $layout = Join-Path $work "layout\$arch"
        New-Item -ItemType Directory -Force -Path $layout | Out-Null
        Copy-Item -LiteralPath $exe -Destination $layout
        Copy-Item -LiteralPath (Join-Path $packagingDir 'Assets') -Destination $layout -Recurse
        $values = @{
            IDENTITY_NAME          = $identityName
            PUBLISHER              = $publisher
            PUBLISHER_DISPLAY_NAME = $publisherDisplayName
            PACKAGE_VERSION        = $packageVersion
            ARCH                   = $msixArch[$arch]
        }
        $manifest = [regex]::Replace($template, '@([A-Z_]+)@', {
                param($match)
                $key = $match.Groups[1].Value
                if (-not $values.ContainsKey($key)) { throw "unknown manifest placeholder @$key@" }
                [System.Security.SecurityElement]::Escape($values[$key])
            })
        Set-Content -Encoding utf8 -LiteralPath (Join-Path $layout 'AppxManifest.xml') -Value $manifest
        $package = Join-Path $bundleInput "kander_${packageVersion}_$($msixArch[$arch]).msix"
        Invoke-Tool $makeAppx @('pack', '/o', '/h', 'SHA256', '/d', $layout, '/p', $package)
    }
    Invoke-Tool $makeAppx @('bundle', '/o', '/bv', $packageVersion, '/d', $bundleInput, '/p', $bundle)
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}

# A bundle is a zip; list it and confirm every requested architecture is in it.
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [System.IO.Compression.ZipFile]::OpenRead($bundle)
try { $entries = @($zip.Entries | ForEach-Object { $_.FullName }) } finally { $zip.Dispose() }
Write-Output "bundle $bundleName contains:"
$entries | ForEach-Object { Write-Output "  $_" }
foreach ($arch in $archList) {
    $expected = "kander_${packageVersion}_$($msixArch[$arch]).msix"
    if ($entries -notcontains $expected) { throw "bundle is missing $expected" }
}

if ($TestSign) {
    $signTool = Find-SdkTool 'signtool.exe'
    $cert = New-SelfSignedCertificate -Type Custom -Subject $publisher -KeyUsage DigitalSignature `
        -FriendlyName 'Kander MSIX local test' -CertStoreLocation 'Cert:\CurrentUser\My' `
        -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.3', '2.5.29.19={text}')
    $certPath = "Cert:\CurrentUser\My\$($cert.Thumbprint)"
    try {
        Export-Certificate -Cert $cert -FilePath (Join-Path $OutDir 'kander-msix-test.cer') | Out-Null
        Invoke-Tool $signTool @('sign', '/fd', 'SHA256', '/sha1', $cert.Thumbprint, '/s', 'My', $bundle)
    } finally {
        Remove-Item -LiteralPath $certPath -DeleteKey -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $certPath) { throw "test certificate $($cert.Thumbprint) is still in the user store" }
    Write-Output "test certificate $($cert.Thumbprint) and its private key removed from the user store"
}

$sums = @(Get-ChildItem -LiteralPath $OutDir -File | Where-Object Name -ne 'SHA256SUMS' | Sort-Object Name | ForEach-Object {
        '{0}  {1}' -f (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant(), $_.Name
    })
Set-Content -Encoding ascii -LiteralPath (Join-Path $OutDir 'SHA256SUMS') -Value $sums
Write-Output "wrote $bundle"
