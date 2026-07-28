[CmdletBinding()]
param(
    [string]$Version = "1.2.0",
    [string]$GoExe = "",
    [string]$PlatformBin = "C:\Program Files\1cv8\8.3.27.2214\bin",
    [string]$BuildInfoBase = ".integration\base"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$requiredPlatform = "8.3.27.2214"

if ([string]::IsNullOrWhiteSpace($GoExe)) {
    $command = Get-Command go -ErrorAction SilentlyContinue
    if ($null -ne $command) { $GoExe = $command.Source }
    else { throw "Go 1.25 is required. Install it in PATH or pass -GoExe with the full path to go.exe." }
}

$actualVersion = & $GoExe version
if ($LASTEXITCODE -ne 0 -or $actualVersion -notmatch "go1\.25(\.| )") { throw "Go 1.25.x is required; found: $actualVersion" }
$oneC = Join-Path $PlatformBin "1cv8.exe"
if (-not (Test-Path -LiteralPath $oneC) -or $PlatformBin -notmatch [regex]::Escape($requiredPlatform)) { throw "1C platform $requiredPlatform is required at $PlatformBin." }

function Format-ProcessArgument([string]$Value) {
    if ($Value.Contains('"')) { throw "Double quotes are not supported in 1C command arguments." }
    if ($Value -notmatch '\s' -and $Value.Length -gt 0) { return $Value }
    return '"' + $Value + '"'
}

function Invoke-Designer([string[]]$Arguments) {
    $formatted = ($Arguments | ForEach-Object { Format-ProcessArgument $_ }) -join ' '
    $process = Start-Process -FilePath $oneC -ArgumentList $formatted -WindowStyle Hidden -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "1C Designer failed with exit code $($process.ExitCode)." }
}

Push-Location $projectRoot
try {
    & $GoExe test ./...
    if ($LASTEXITCODE -ne 0) { throw "Go tests failed." }
    & $GoExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw "Go vet failed." }
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File installer\Install-MCP1CData.ps1 -SelfTest
    if ($LASTEXITCODE -ne 0) { throw "Installer self-test failed." }

    $dist = Join-Path $projectRoot "dist"
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    $stage = Join-Path $dist ("MCP1CData-Setup-{0}" -f $Version)
    if (Test-Path -LiteralPath $stage) {
        $resolvedStage = [IO.Path]::GetFullPath($stage)
        $allowed = [IO.Path]::GetFullPath($dist) + [IO.Path]::DirectorySeparatorChar
        if (-not $resolvedStage.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase)) { throw "Unsafe staging path." }
        Remove-Item -LiteralPath $resolvedStage -Recurse -Force
    }
    $payload = Join-Path $stage "payload"
    New-Item -ItemType Directory -Force -Path $payload | Out-Null

    $core = Join-Path $payload "mcp-1c-data.exe"
    $launcher = Join-Path $payload "mcp-1c-data-launcher.exe"
    & $GoExe build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$Version" -o $core ./cmd/mcp-1c-data
    if ($LASTEXITCODE -ne 0) { throw "Core MCP build failed." }
    & $GoExe build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$Version" -o $launcher ./cmd/mcp-1c-data-launcher
    if ($LASTEXITCODE -ne 0) { throw "Launcher build failed." }

    $resolvedBase = (Resolve-Path -LiteralPath $BuildInfoBase).Path
    if (-not (Test-Path -LiteralPath (Join-Path $resolvedBase "1Cv8.1CD"))) { throw "BuildInfoBase must be a disposable file infobase." }
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File extension\install.ps1 -InfoBasePath $resolvedBase -PlatformBin $PlatformBin
    if ($LASTEXITCODE -ne 0) { throw "Extension source validation failed." }
    $cfe = Join-Path $payload "MCP_DataAccess.cfe"
    $dumpLog = Join-Path $stage "dump-cfe.log"
    Invoke-Designer @("DESIGNER", "/F", $resolvedBase, "/WA+", "/DisableStartupMessages", "/DisableStartupDialogs", "/DumpCfg", $cfe, "-Extension", "MCP_DataAccess", "/Out", $dumpLog)
    if (-not (Test-Path -LiteralPath $cfe) -or (Get-Item -LiteralPath $cfe).Length -eq 0) { throw "CFE export was not created." }

    $cfeLoadLog = Join-Path $stage "load-cfe-test.log"
    Invoke-Designer @("DESIGNER", "/F", $resolvedBase, "/WA+", "/DisableStartupMessages", "/DisableStartupDialogs", "/LoadCfg", $cfe, "-Extension", "MCP_DataAccess", "/Out", $cfeLoadLog)
    $cfeUpdateLog = Join-Path $stage "update-cfe-test.log"
    Invoke-Designer @("DESIGNER", "/F", $resolvedBase, "/WA+", "/DisableStartupMessages", "/DisableStartupDialogs", "/UpdateDBCfg", "-Extension", "MCP_DataAccess", "/Out", $cfeUpdateLog)
    $cfeCheckLog = Join-Path $stage "check-cfe-test.log"
    Invoke-Designer @("DESIGNER", "/F", $resolvedBase, "/WA+", "/DisableStartupMessages", "/DisableStartupDialogs", "/CheckConfig", "-AllExtensions", "-CheckModules", "-IncorrectReferences", "-HandlersExistence", "/Out", $cfeCheckLog)

    Copy-Item -LiteralPath installer\Setup.cmd -Destination (Join-Path $stage "Setup.cmd")
    Copy-Item -LiteralPath installer\Install-MCP1CData.ps1 -Destination (Join-Path $stage "Install-MCP1CData.ps1")
    Copy-Item -LiteralPath docs\INSTALLER_GUIDE.md -Destination (Join-Path $stage "README-INSTALL.txt")
    $artifacts = foreach ($file in @($core, $launcher, $cfe)) {
        [ordered]@{ name = Split-Path -Leaf $file; size = (Get-Item -LiteralPath $file).Length; sha256 = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash }
    }
    $manifest = [ordered]@{
        product = "MCP1CData"; version = $Version; platform = $requiredPlatform
        apache = [ordered]@{ version = "2.4.68-260617 Win64 VS18"; url = "https://www.apachelounge.com/download/VS18/binaries/httpd-2.4.68-260617-Win64-VS18.zip"; sha256 = "EAC38A9F8D21B6BA0817CD1576A3D291CC771AFD3C6A210D120BDEA6F4AA4AEB" }
        artifacts = @($artifacts)
    }
    [IO.File]::WriteAllText((Join-Path $stage "manifest.json"), ($manifest | ConvertTo-Json -Depth 5), (New-Object Text.UTF8Encoding($false)))
    $zip = Join-Path $dist ("MCP1CData-Setup-{0}.zip" -f $Version)
    if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
    Compress-Archive -LiteralPath $stage -DestinationPath $zip -CompressionLevel Optimal
    Write-Host "Built installer: $zip"
} finally {
    Pop-Location
}
