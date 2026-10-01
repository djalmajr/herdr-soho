param(
    [string]$Version,
    [string]$InstallDir = $env:HERDR_SOHO_INSTALL_DIR,
    [switch]$AddToPath
)

$ErrorActionPreference = 'Stop'

if ($Version) {
    if ($Version -notmatch '^v[0-9A-Za-z._-]+$' -or $Version.Contains('..')) {
        throw 'install.ps1: -Version must be a valid release tag beginning with v.'
    }
}

$os = 'windows'
switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "install.ps1: unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\herdr-soho'
}
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$destination = Join-Path $InstallDir 'herdr-soho.exe'
$artifact = "herdr-soho_${os}_${arch}.exe"
$releaseBase = $env:HERDR_SOHO_RELEASE_BASE
if (-not $releaseBase) {
    $releaseBase = 'https://github.com/djalmajr/herdr-soho/releases'
}
if ($releaseBase -match '\s') {
    throw 'install.ps1: release base URL contains whitespace.'
}
if ($Version) {
	$releaseUrl = "$($releaseBase.TrimEnd('/'))/download/$Version"
} else {
    $releaseUrl = "$($releaseBase.TrimEnd('/'))/latest/download"
}

[System.IO.Directory]::CreateDirectory($InstallDir) | Out-Null
$nonce = [Guid]::NewGuid().ToString('N')
$binaryTemp = Join-Path $InstallDir ".herdr-soho.$nonce.tmp"
$sumsTemp = Join-Path $InstallDir ".herdr-soho.$nonce.sums"
try {
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseUrl/$artifact" -OutFile $binaryTemp
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseUrl/SHA256SUMS" -OutFile $sumsTemp

    $matches = @(Get-Content -LiteralPath $sumsTemp | Where-Object {
        $_ -match ('^([0-9A-Fa-f]{64})\s+\*?' + [regex]::Escape($artifact) + '$')
    })
    if ($matches.Count -ne 1) {
        throw "install.ps1: SHA256SUMS has no unique entry for $artifact."
    }
    $expected = [regex]::Match($matches[0], '^([0-9A-Fa-f]{64})').Groups[1].Value.ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $binaryTemp -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "install.ps1: sha256 mismatch for $artifact."
    }

    if ([System.IO.File]::Exists($destination)) {
        # Windows PowerShell turns $null into '' for a .NET string parameter, and
        # File.Replace rejects '' as a backup path ("The path is not of a legal
        # form"); [NullString]::Value passes a real null (no backup file).
        [System.IO.File]::Replace($binaryTemp, $destination, [NullString]::Value)
    } else {
        [System.IO.File]::Move($binaryTemp, $destination)
    }
} finally {
    Remove-Item -LiteralPath $binaryTemp, $sumsTemp -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathParts = @($userPath -split ';' | Where-Object { $_ })
$inUserPath = $pathParts | Where-Object { [string]::Equals($_.TrimEnd('\'), $InstallDir.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase) }
if ($AddToPath) {
    if (-not $inUserPath) {
        $updatedPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable('Path', $updatedPath, 'User')
    }
    if (($env:Path -split ';' | Where-Object { [string]::Equals($_.TrimEnd('\'), $InstallDir.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase) }) -eq $null) {
        $env:Path = if ($env:Path) { "$env:Path;$InstallDir" } else { $InstallDir }
    }
} elseif (-not $inUserPath) {
    Write-Output "Add $InstallDir to your user PATH to run herdr-soho from any shell."
}

& $destination --version
if ($LASTEXITCODE -ne 0) {
    throw "install.ps1: installed herdr-soho --version exited with $LASTEXITCODE."
}
