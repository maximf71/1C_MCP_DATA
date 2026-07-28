[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InfoBasePath,

    [string]$PlatformBin = "C:\Program Files\1cv8\8.3.27.2214\bin",

    [string]$UserName = "",

    [string]$PasswordEnv = "MCP_1C_INSTALL_PASSWORD"
)

$ErrorActionPreference = "Stop"
$extensionName = "MCP_DataAccess"
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$sourceDir = Join-Path $scriptRoot "src"
$oneC = Join-Path $PlatformBin "1cv8.exe"
$resolvedBase = (Resolve-Path -LiteralPath $InfoBasePath).Path

if (-not (Test-Path -LiteralPath $oneC)) {
    throw "Required platform was not found: $oneC"
}
if ($PlatformBin -notmatch '8\.3\.27\.2214') {
    throw "This pilot is restricted to platform 8.3.27.2214."
}
if (-not (Test-Path -LiteralPath (Join-Path $resolvedBase "1Cv8.1CD"))) {
    throw "InfoBasePath must point to a file infobase."
}

$logDir = Join-Path $scriptRoot "logs"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$loadLog = Join-Path $logDir "load-extension.log"
$checkLog = Join-Path $logDir "check-extension.log"
$updateLog = Join-Path $logDir "update-extension.log"
$auth = @()
if ([string]::IsNullOrWhiteSpace($UserName)) {
    $auth += "/WA+"
} else {
    $auth += "/N"
    $auth += $UserName
    $Password = [Environment]::GetEnvironmentVariable($PasswordEnv)
    if (-not [string]::IsNullOrEmpty($Password)) {
        $auth += "/P"
        $auth += $Password
    }
}

function Format-ProcessArgument([string]$Value) {
    if ($Value.Contains('"')) {
        throw "Double quotes are not supported in 1C command arguments."
    }
    if ($Value -notmatch '\s' -and $Value.Length -gt 0) {
        return $Value
    }
    $trailingSlashes = [regex]::Match($Value, '\\+$').Value
    if ($trailingSlashes.Length -gt 0) {
        $Value = $Value.Substring(0, $Value.Length - $trailingSlashes.Length) + ($trailingSlashes * 2)
    }
    return '"' + $Value + '"'
}

function Invoke-Designer([string[]]$Arguments) {
    $formatted = ($Arguments | ForEach-Object { Format-ProcessArgument $_ }) -join ' '
    $process = Start-Process -FilePath $oneC -ArgumentList $formatted -WindowStyle Hidden -Wait -PassThru
    return $process.ExitCode
}

$loadArguments = @(
    "DESIGNER", "/F", $resolvedBase
) + $auth + @(
    "/DisableStartupMessages", "/DisableStartupDialogs",
    "/LoadConfigFromFiles", $sourceDir, "-Extension", $extensionName,
    "/Out", $loadLog
)
$loadExitCode = Invoke-Designer $loadArguments
if ($loadExitCode -ne 0) {
    throw "Extension load failed. See $loadLog"
}

$updateArguments = @(
    "DESIGNER", "/F", $resolvedBase
) + $auth + @(
    "/DisableStartupMessages", "/DisableStartupDialogs",
    "/UpdateDBCfg", "-Extension", $extensionName,
    "/Out", $updateLog
)
$updateExitCode = Invoke-Designer $updateArguments
if ($updateExitCode -ne 0) {
    throw "Extension database activation failed. See $updateLog"
}

$checkArguments = @(
    "DESIGNER", "/F", $resolvedBase
) + $auth + @(
    "/DisableStartupMessages", "/DisableStartupDialogs",
    "/CheckConfig", "-AllExtensions", "-CheckModules", "-IncorrectReferences",
    "-HandlersExistence", "/Out", $checkLog
)
$checkExitCode = Invoke-Designer $checkArguments
if ($checkExitCode -ne 0) {
    throw "Extension validation failed. See $checkLog"
}
$checkText = Get-Content -LiteralPath $checkLog -Encoding Unicode -Raw
$successMarker = -join ([char[]]@(
    0x041E, 0x0448, 0x0438, 0x0431, 0x043E, 0x043A, 0x0020, 0x043D, 0x0435,
    0x0020, 0x043E, 0x0431, 0x043D, 0x0430, 0x0440, 0x0443, 0x0436, 0x0435,
    0x043D, 0x043E
))
if (-not $checkText.Contains($successMarker)) {
    throw "Extension validation reported errors. See $checkLog"
}

Write-Host "Installed and checked extension $extensionName."
Write-Host "Republish the infobase with HTTP services enabled before starting the MCP server."
