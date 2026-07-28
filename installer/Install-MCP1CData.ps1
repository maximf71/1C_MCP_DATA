[CmdletBinding()]
param(
    [switch]$SelfTest,
    [switch]$CheckEnvironment,
    [string]$InfoBasePath = "",
    [string]$ResumeProfile = "",
    [switch]$UpdateCodexOnResume
)

$ErrorActionPreference = "Stop"
$script:ProductVersion = "1.2.0"
$script:RequiredPlatformVersion = "8.3.27.2214"
$script:ApacheUrl = "https://www.apachelounge.com/download/VS18/binaries/httpd-2.4.68-260617-Win64-VS18.zip"
$script:ApacheSha256 = "EAC38A9F8D21B6BA0817CD1576A3D291CC771AFD3C6A210D120BDEA6F4AA4AEB"
$script:ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$script:PayloadRoot = Join-Path $script:ScriptRoot "payload"
$script:LocalRoot = Join-Path $env:LOCALAPPDATA "MCP1CData"
$script:PlatformBin = "C:\Program Files\1cv8\$($script:RequiredPlatformVersion)\bin"
$script:LogFile = $null

function Write-SafeLog {
    param([string]$Message, [ValidateSet("INFO", "WARN", "ERROR")][string]$Level = "INFO")
    $line = "{0} [{1}] {2}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $Level, $Message
    if ($script:LogFile) {
        Add-Content -LiteralPath $script:LogFile -Value $line -Encoding UTF8
    }
    if ($script:ProgressCallback) {
        & $script:ProgressCallback $line
    } else {
        Write-Host $line
    }
}

function ConvertTo-ProfileSlug {
    param([Parameter(Mandatory = $true)][string]$Value)
    $normalized = $Value.ToLowerInvariant().Normalize([Text.NormalizationForm]::FormD)
    $translit = @{
        'а'='a'; 'б'='b'; 'в'='v'; 'г'='g'; 'д'='d'; 'е'='e'; 'ж'='zh'; 'з'='z'; 'и'='i'; 'й'='y';
        'к'='k'; 'л'='l'; 'м'='m'; 'н'='n'; 'о'='o'; 'п'='p'; 'р'='r'; 'с'='s'; 'т'='t'; 'у'='u';
        'ф'='f'; 'х'='h'; 'ц'='c'; 'ч'='ch'; 'ш'='sh'; 'щ'='sch'; 'ъ'=''; 'ы'='y'; 'ь'=''; 'э'='e';
        'ю'='yu'; 'я'='ya'
    }
    $builder = New-Object Text.StringBuilder
    foreach ($character in $normalized.ToCharArray()) {
        $category = [Globalization.CharUnicodeInfo]::GetUnicodeCategory($character)
        if ($category -eq [Globalization.UnicodeCategory]::NonSpacingMark) { continue }
        if (($character -ge 'a' -and $character -le 'z') -or [char]::IsDigit($character)) { [void]$builder.Append($character) }
        elseif ($translit.ContainsKey([string]$character)) { [void]$builder.Append($translit[[string]$character]) }
        elseif ($builder.Length -gt 0 -and $builder[$builder.Length - 1] -ne '_') { [void]$builder.Append('_') }
    }
    $result = $builder.ToString().Trim('_')
    if ($result.Length -gt 40) { $result = $result.Substring(0, 40).TrimEnd('_') }
    if ([string]::IsNullOrWhiteSpace($result)) { $result = "base" }
    return $result
}

function Quote-TomlString {
    param([string]$Value)
    return '"' + $Value.Replace('\', '\\').Replace('"', '\"') + '"'
}

function New-CodexManagedBlock {
    param([string]$ProfileId, [string]$LauncherPath, [string]$ProfilePath)
    $serverName = "onec_data_$(ConvertTo-ProfileSlug $ProfileId)"
    $lines = @(
        "# >>> MCP1CData:$ProfileId >>>",
        "[mcp_servers.$serverName]",
        "command = $(Quote-TomlString $LauncherPath)",
        "args = [$(Quote-TomlString '--profile'), $(Quote-TomlString $ProfilePath)]",
        "# <<< MCP1CData:$ProfileId <<<"
    )
    return ($lines -join [Environment]::NewLine)
}

function Set-CodexManagedBlock {
    param([string]$ConfigPath, [string]$ProfileId, [string]$LauncherPath, [string]$ProfilePath)
    $parent = Split-Path -Parent $ConfigPath
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    $original = if (Test-Path -LiteralPath $ConfigPath) { [IO.File]::ReadAllText($ConfigPath) } else { "" }
    $timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $backup = $null
    if (Test-Path -LiteralPath $ConfigPath) {
        $backup = "$ConfigPath.mcp1cdata-$timestamp.bak"
        Copy-Item -LiteralPath $ConfigPath -Destination $backup
    }
    $escapedId = [regex]::Escape($ProfileId)
    $pattern = "(?ms)^# >>> MCP1CData:$escapedId >>>\r?\n.*?^# <<< MCP1CData:$escapedId <<<\r?\n?"
    $withoutOldBlock = [regex]::Replace($original, $pattern, "").TrimEnd("`r", "`n")
    $block = New-CodexManagedBlock $ProfileId $LauncherPath $ProfilePath
    $updated = if ($withoutOldBlock.Length -eq 0) { $block + [Environment]::NewLine } else { $withoutOldBlock + [Environment]::NewLine + [Environment]::NewLine + $block + [Environment]::NewLine }
    [IO.File]::WriteAllText($ConfigPath, $updated, (New-Object Text.UTF8Encoding($false)))
    return $backup
}

function Protect-Credentials {
    param([string]$User, [string]$Password, [string]$ProfileId)
    Add-Type -AssemblyName System.Security
    $json = @{ user = $User; password = $Password } | ConvertTo-Json -Compress
    $plain = [Text.Encoding]::UTF8.GetBytes($json)
    $entropy = [Text.Encoding]::UTF8.GetBytes("MCP1CData/profile/v1/$ProfileId")
    try {
        return [Security.Cryptography.ProtectedData]::Protect($plain, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
    } finally {
        [Array]::Clear($plain, 0, $plain.Length)
        [Array]::Clear($entropy, 0, $entropy.Length)
        $json = $null
    }
}

function Unprotect-CredentialsForTest {
    param([byte[]]$Cipher, [string]$ProfileId)
    Add-Type -AssemblyName System.Security
    $entropy = [Text.Encoding]::UTF8.GetBytes("MCP1CData/profile/v1/$ProfileId")
    return [Security.Cryptography.ProtectedData]::Unprotect($Cipher, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
}

function Get-ReservedProfilePorts {
    param([string]$ExcludeProfileId = "")
    $result = New-Object 'Collections.Generic.HashSet[int]'
    $profilesDirectory = Join-Path $script:LocalRoot "profiles"
    if (-not (Test-Path -LiteralPath $profilesDirectory)) { return $result }
    foreach ($profileFile in Get-ChildItem -LiteralPath $profilesDirectory -File -Filter "*.json" -ErrorAction SilentlyContinue) {
        try {
            $profile = [IO.File]::ReadAllText($profileFile.FullName, [Text.Encoding]::UTF8) | ConvertFrom-Json
            if (-not [string]::IsNullOrWhiteSpace($ExcludeProfileId) -and [string]$profile.id -eq $ExcludeProfileId) { continue }
            $uri = [Uri]([string]$profile.base_url)
            if ($uri.IsLoopback -and $uri.Port -ge 1) { [void]$result.Add($uri.Port) }
        } catch {
            # Поврежденный посторонний профиль не должен раскрывать данные и не блокирует проверку свободного порта.
        }
    }
    return $result
}

function Get-FreeLoopbackPort {
    param([int]$Preferred = 8080, [string]$ExcludeProfileId = "")
    $reservedPorts = @(Get-ReservedProfilePorts $ExcludeProfileId)
    foreach ($port in $Preferred..8099) {
        if ($reservedPorts -contains $port) { continue }
        $listener = $null
        try {
            $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, $port)
            $listener.Start()
            return $port
        } catch {
        } finally {
            if ($listener) { $listener.Stop() }
        }
    }
    throw "Нет свободного порта в диапазоне 8080-8099."
}

function Assert-InstallPreflight {
    param([Parameter(Mandatory = $true)][string]$BasePath, [switch]$SkipPayload)
    $resolvedBase = (Resolve-Path -LiteralPath $BasePath).Path
    if (-not (Test-Path -LiteralPath (Join-Path $resolvedBase "1Cv8.1CD") -PathType Leaf)) {
        throw "Выбранный каталог не является файловой базой 1С: отсутствует 1Cv8.1CD."
    }
    $oneC = Join-Path $script:PlatformBin "1cv8.exe"
    if (-not (Test-Path -LiteralPath $oneC -PathType Leaf)) {
        throw "Требуется платформа 1С $($script:RequiredPlatformVersion): $oneC"
    }
    $actual = (Get-Item -LiteralPath $oneC).VersionInfo.ProductVersion
    if ($actual -notlike "$($script:RequiredPlatformVersion)*") {
        throw "Обнаружена неподдерживаемая версия платформы: $actual. Требуется $($script:RequiredPlatformVersion)."
    }
    $wsap = Join-Path $script:PlatformBin "wsap24.dll"
    if (-not (Test-Path -LiteralPath $wsap -PathType Leaf)) {
        throw "В установке 1С $($script:RequiredPlatformVersion) отсутствует wsap24.dll. Добавьте компонент 'Модули расширения веб-сервера' через установщик этой же версии 1С и повторите запуск. Файл другой версии копировать нельзя."
    }
    if (-not $SkipPayload) {
        foreach ($file in @("mcp-1c-data.exe", "mcp-1c-data-launcher.exe", "MCP_DataAccess.cfe")) {
            if (-not (Test-Path -LiteralPath (Join-Path $script:PayloadRoot $file) -PathType Leaf)) {
                throw "Не найден payload\$file. Полностью распакуйте архив MCP1CData-Setup-$($script:ProductVersion).zip и запускайте Setup.cmd из распакованной папки."
            }
        }
    }
    return @{ BasePath = $resolvedBase; OneC = $oneC; Wsap = $wsap }
}

function Format-ProcessArgument {
    param([string]$Value)
    if ($Value.Contains('"')) { throw "Двойные кавычки в аргументах 1С не поддерживаются мастером." }
    if ($Value -notmatch '\s' -and $Value.Length -gt 0) { return $Value }
    $trailing = [regex]::Match($Value, '\\+$').Value
    if ($trailing.Length -gt 0) { $Value = $Value.Substring(0, $Value.Length - $trailing.Length) + ($trailing * 2) }
    return '"' + $Value + '"'
}

function Invoke-1CDesigner {
    param([string]$OneC, [string[]]$Arguments, [string]$Operation)
    $formatted = ($Arguments | ForEach-Object { Format-ProcessArgument $_ }) -join ' '
    Write-SafeLog "$Operation..."
    $process = Start-Process -FilePath $OneC -ArgumentList $formatted -WindowStyle Hidden -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        $outIndex = [Array]::IndexOf($Arguments, "/Out")
        $detail = if ($outIndex -ge 0 -and $outIndex + 1 -lt $Arguments.Count) { " Журнал 1С: $($Arguments[$outIndex + 1])" } else { "" }
        throw "$Operation завершилась с кодом $($process.ExitCode).$detail"
    }
}

function New-DesignerAuthArguments {
    param([string]$User, [string]$Password)
    if ([string]::IsNullOrWhiteSpace($User)) { return @("/WA+") }
    $result = @("/N", $User)
    if (-not [string]::IsNullOrEmpty($Password)) { $result += @("/P", $Password) }
    return $result
}

function Backup-InfoBase {
    param([string]$OneC, [string]$BasePath, [string]$BackupDirectory, [string[]]$DesignerAuth)
    New-Item -ItemType Directory -Force -Path $BackupDirectory | Out-Null
    $backupPath = Join-Path $BackupDirectory ("{0}-{1}.dt" -f (Split-Path -Leaf $BasePath), (Get-Date -Format "yyyyMMdd-HHmmss"))
    $logPath = "$backupPath.log"
    Invoke-1CDesigner $OneC (@("DESIGNER", "/F", $BasePath) + $DesignerAuth + @("/DisableStartupMessages", "/DisableStartupDialogs", "/DumpIB", $backupPath, "/Out", $logPath)) "Обязательная резервная копия DT"
    if (-not (Test-Path -LiteralPath $backupPath -PathType Leaf) -or (Get-Item -LiteralPath $backupPath).Length -eq 0) {
        throw "Резервная копия DT не создана; установка остановлена до изменения базы."
    }
    Write-SafeLog "Резервная копия создана: $backupPath"
    return $backupPath
}

function Install-ExtensionCFE {
    param([string]$OneC, [string]$BasePath, [string]$CFEPath, [string]$LogDirectory, [string[]]$DesignerAuth)
    $loadLog = Join-Path $LogDirectory "load-extension.log"
    Invoke-1CDesigner $OneC (@("DESIGNER", "/F", $BasePath) + $DesignerAuth + @("/DisableStartupMessages", "/DisableStartupDialogs", "/LoadCfg", $CFEPath, "-Extension", "MCP_DataAccess", "/Out", $loadLog)) "Установка расширения MCP_DataAccess"
    $updateLog = Join-Path $LogDirectory "update-extension.log"
    Invoke-1CDesigner $OneC (@("DESIGNER", "/F", $BasePath) + $DesignerAuth + @("/DisableStartupMessages", "/DisableStartupDialogs", "/UpdateDBCfg", "-Extension", "MCP_DataAccess", "/Out", $updateLog)) "Активация расширения в базе данных"
    $checkLog = Join-Path $LogDirectory "check-extension.log"
    Invoke-1CDesigner $OneC (@("DESIGNER", "/F", $BasePath) + $DesignerAuth + @("/DisableStartupMessages", "/DisableStartupDialogs", "/CheckConfig", "-AllExtensions", "-CheckModules", "-IncorrectReferences", "-HandlersExistence", "/Out", $checkLog)) "Проверка расширения"
}

function Install-PinnedApache {
    param([string]$DestinationRoot)
    $cache = Join-Path $script:LocalRoot "cache"
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
    $archive = Join-Path $cache "httpd-2.4.68-260617-Win64-VS18.zip"
    if (-not (Test-Path -LiteralPath $archive)) {
        Write-SafeLog "Загрузка Apache Lounge 2.4.68..."
        $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
        if ($null -eq $curl) { throw "Для безопасной загрузки Apache требуется системный curl.exe (входит в поддерживаемые версии Windows)." }
        $partial = "$archive.part"
        if (Test-Path -LiteralPath $partial) { Remove-Item -LiteralPath $partial -Force }
        & $curl.Source --fail --location --proto '=https' --tlsv1.2 --retry 2 --silent --show-error --output $partial $script:ApacheUrl
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $partial) -or (Get-Item -LiteralPath $partial).Length -eq 0) {
            throw "Не удалось загрузить закрепленный архив Apache по HTTPS."
        }
        Move-Item -LiteralPath $partial -Destination $archive
    }
    $hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($hash -ne $script:ApacheSha256) {
        throw "Контрольная сумма Apache не совпала. Ожидалась $($script:ApacheSha256), получена $hash."
    }
    Write-SafeLog "SHA-256 Apache подтвержден."
    $extractRoot = Join-Path $script:LocalRoot "apache-extract"
    if (Test-Path -LiteralPath $extractRoot) {
        $resolved = [IO.Path]::GetFullPath($extractRoot)
        $allowed = [IO.Path]::GetFullPath($script:LocalRoot) + [IO.Path]::DirectorySeparatorChar
        if (-not $resolved.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase)) { throw "Небезопасный путь временного каталога Apache." }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
    Expand-Archive -LiteralPath $archive -DestinationPath $extractRoot -Force
    $source = Join-Path $extractRoot "Apache24"
    if (-not (Test-Path -LiteralPath (Join-Path $source "bin\httpd.exe"))) { throw "Архив Apache имеет неожиданную структуру." }
    if (Test-Path -LiteralPath $DestinationRoot) {
        $httpd = Join-Path $DestinationRoot "bin\httpd.exe"
        $requiredExistingFiles = @(
            $httpd,
            (Join-Path $DestinationRoot "conf\httpd.conf"),
            (Join-Path $DestinationRoot "modules\mod_authz_core.so")
        )
        $existingComplete = ($requiredExistingFiles | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -eq 0
        if ($existingComplete) {
            $sourceHash = (Get-FileHash -LiteralPath (Join-Path $source "bin\httpd.exe") -Algorithm SHA256).Hash
            $existingHash = (Get-FileHash -LiteralPath $httpd -Algorithm SHA256).Hash
            if ($sourceHash -eq $existingHash) {
                Write-SafeLog "Уже установленный Apache 2.4.68 проверен и будет переиспользован."
                return $DestinationRoot
            }
        }
        $pidFile = Join-Path $script:LocalRoot "logs\httpd.pid"
        if (Test-Path -LiteralPath $pidFile) {
            $apachePid = 0
            if ([int]::TryParse(([IO.File]::ReadAllText($pidFile).Trim()), [ref]$apachePid)) {
                $process = Get-Process -Id $apachePid -ErrorAction SilentlyContinue
                if ($process) {
                    $processPath = $null
                    try { $processPath = $process.Path } catch {}
                    if ($processPath -and ([IO.Path]::GetFullPath($processPath) -eq [IO.Path]::GetFullPath($httpd))) {
                        Stop-Process -Id $apachePid -Force
                        $process.WaitForExit(10000) | Out-Null
                    }
                }
            }
        }
        $resolved = [IO.Path]::GetFullPath($DestinationRoot)
        $allowed = [IO.Path]::GetFullPath($script:LocalRoot) + [IO.Path]::DirectorySeparatorChar
        if (-not $resolved.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase)) { throw "Небезопасный путь Apache." }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
    Copy-Item -LiteralPath $source -Destination $DestinationRoot -Recurse
    return $DestinationRoot
}

function ConvertTo-ApachePath { param([string]$Path) return ([IO.Path]::GetFullPath($Path) -replace '\\', '/') }

function New-VrdText {
    param([string]$BasePath, [string]$PublicationName)
    $ib = "File=&quot;$([Security.SecurityElement]::Escape($BasePath))&quot;;"
    return @"
<?xml version="1.0" encoding="UTF-8"?>
<point xmlns="http://v8.1c.ru/8.2/virtual-resource-system" base="/$PublicationName" ib="$ib">
  <ws enable="false"/>
  <httpServices publishByDefault="false" publishExtensionsByDefault="true">
    <service name="MCPDataService" rootUrl="mcp-data" enable="true"/>
  </httpServices>
  <standardOdata enable="false"/>
</point>
"@.Trim()
}

function New-ApacheConfigurationText {
    param([string]$BaseContent, [string]$ServerRoot, [string]$LogDir, [string]$WsapPath, [string]$PublicationPath, [string]$VrdPath, [string]$PublicationName, [int]$Port)
    $result = [regex]::Replace($BaseContent, '(?m)^\s*Define\s+SRVROOT\s+.*$', "Define SRVROOT `"$ServerRoot`"")
    $result = [regex]::Replace($result, '(?m)^\s*Listen\s+.*$', '# Listen disabled by MCP1CData installer')
    return $result + @"

# MCP1CData managed section. Do not expose this listener outside loopback.
Listen 127.0.0.1:$Port
ServerName 127.0.0.1:$Port
LoadModule _1cws_module "$WsapPath"
PidFile "$LogDir/httpd-$PublicationName.pid"
ErrorLog "$LogDir/apache-error-$PublicationName.log"
LogLevel warn
<Directory "$ServerRoot/htdocs">
    Require all denied
</Directory>
Alias "/$PublicationName" "$PublicationPath/"
<Directory "$PublicationPath/">
    AllowOverride None
    Options None
    Require all granted
    SetHandler 1c-application
    ManagedApplicationDescriptor "$VrdPath"
</Directory>
<Location "/$PublicationName">
    Require all denied
</Location>
<Location "/$PublicationName/hs/mcp-data">
    Require all granted
</Location>
"@
}

function Write-ApachePublication {
    param([string]$ApacheRoot, [string]$BasePath, [string]$WsapPath, [string]$PublicationName, [int]$Port)
    $publication = Join-Path $script:LocalRoot "publications\$PublicationName"
    New-Item -ItemType Directory -Force -Path $publication | Out-Null
    $vrdPath = Join-Path $publication "default.vrd"
    $vrd = New-VrdText $BasePath $PublicationName
    [IO.File]::WriteAllText($vrdPath, $vrd, (New-Object Text.UTF8Encoding($false)))
    $baseConf = Join-Path $ApacheRoot "conf\httpd.conf"
    $confPath = Join-Path $ApacheRoot "conf\httpd-mcp1cdata-$PublicationName.conf"
    $logDir = ConvertTo-ApachePath (Join-Path $script:LocalRoot "logs")
    $publicationApache = ConvertTo-ApachePath $publication
    $wsapApache = ConvertTo-ApachePath $WsapPath
    $vrdApache = ConvertTo-ApachePath $vrdPath
    $serverRoot = ConvertTo-ApachePath $ApacheRoot
    $baseContent = [IO.File]::ReadAllText($baseConf)
    $conf = New-ApacheConfigurationText $baseContent $serverRoot $logDir $wsapApache $publicationApache $vrdApache $PublicationName $Port
    [IO.File]::WriteAllText($confPath, $conf, (New-Object Text.UTF8Encoding($false)))
    $httpd = Join-Path $ApacheRoot "bin\httpd.exe"
    & $httpd -t -f $confPath
    if ($LASTEXITCODE -ne 0) { throw "Apache отклонил сформированную конфигурацию." }
    Start-Process -FilePath $httpd -ArgumentList @("-f", (Format-ProcessArgument $confPath)) -WindowStyle Hidden
    $ready = $false
    foreach ($attempt in 1..20) {
        Start-Sleep -Milliseconds 250
        $client = New-Object Net.Sockets.TcpClient
        try {
            $client.Connect([Net.IPAddress]::Loopback, $Port)
            $ready = $true
            break
        } catch {
        } finally {
            $client.Dispose()
        }
    }
    if (-not $ready) { throw "Apache не открыл локальный порт $Port за 5 секунд." }
    return @{ PublicationPath = $publication; VrdPath = $vrdPath; ConfPath = $confPath; Httpd = $httpd }
}

function Install-PayloadFile {
    param([string]$Source, [string]$Destination)
    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
        $sourceHash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
        $destinationHash = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
        if ($sourceHash -eq $destinationHash) {
            Write-SafeLog "Компонент $(Split-Path -Leaf $Destination) уже установлен и будет переиспользован."
            return
        }
    }
    try {
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    } catch {
        throw "Не удалось обновить $(Split-Path -Leaf $Destination). Закройте использующий его процесс или перезапустите Codex и повторите установку."
    }
}

function Invoke-InfoSmokeTest {
    param([string]$BaseUrl, [string]$User, [string]$Password)
    $pair = [Text.Encoding]::UTF8.GetBytes("$User`:$Password")
    try {
        $auth = [Convert]::ToBase64String($pair)
        $response = Invoke-RestMethod -Method Get -Uri ($BaseUrl.TrimEnd('/') + "/info") -Headers @{ Authorization = "Basic $auth" } -TimeoutSec 30
        if (-not $response.read_only) { throw "HTTP-сервис не подтвердил режим только для чтения." }
        if ($response.platform_version -notlike "$($script:RequiredPlatformVersion)*") { throw "HTTP-сервис работает на платформе $($response.platform_version)." }
        if ([string]::IsNullOrWhiteSpace([string]$response.user)) { throw "HTTP-сервис не вернул пользователя 1С." }
        Write-SafeLog "HTTP smoke-test пройден: пользователь '$($response.user)', read_only=true."
        return $response
    } finally {
        [Array]::Clear($pair, 0, $pair.Length)
        $auth = $null
    }
}

function Install-MCP1CData {
    param([string]$BasePath, [string]$ProfileName, [string]$User, [string]$Password, [string]$InstallerUser, [string]$InstallerPassword, [int]$PreferredPort, [bool]$UpdateCodex, [bool]$CreateBackup = $true)
    $preflight = Assert-InstallPreflight $BasePath
    $profileId = ConvertTo-ProfileSlug $ProfileName
    $port = Get-FreeLoopbackPort $PreferredPort $profileId
    $publicationName = "mcp1c_$profileId"
    $runId = Get-Date -Format "yyyyMMdd-HHmmss"
    $logDirectory = Join-Path $script:LocalRoot "logs\$runId"
    New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
    $script:LogFile = Join-Path $logDirectory "install.log"
    Write-SafeLog "Начата установка профиля '$profileId'. Секреты в журнал не записываются."

    if ([string]::IsNullOrWhiteSpace($InstallerUser)) { $InstallerUser = $User; $InstallerPassword = $Password }
    $designerAuth = New-DesignerAuthArguments $InstallerUser $InstallerPassword
    $backup = $null
    if ($CreateBackup) {
        $backup = Backup-InfoBase $preflight.OneC $preflight.BasePath (Join-Path $script:LocalRoot "backups") $designerAuth
    } else {
        Write-SafeLog "Создание DT-копии пропущено по подтвержденному выбору пользователя." "WARN"
    }
    $binDirectory = Join-Path $script:LocalRoot "bin\$($script:ProductVersion)"
    New-Item -ItemType Directory -Force -Path $binDirectory | Out-Null
    Install-PayloadFile (Join-Path $script:PayloadRoot "mcp-1c-data.exe") (Join-Path $binDirectory "mcp-1c-data.exe")
    Install-PayloadFile (Join-Path $script:PayloadRoot "mcp-1c-data-launcher.exe") (Join-Path $binDirectory "mcp-1c-data-launcher.exe")
    Install-ExtensionCFE $preflight.OneC $preflight.BasePath (Join-Path $script:PayloadRoot "MCP_DataAccess.cfe") $logDirectory $designerAuth

    $apacheRoot = Install-PinnedApache (Join-Path $script:LocalRoot "Apache24")
    $apache = Write-ApachePublication $apacheRoot $preflight.BasePath $preflight.Wsap $publicationName $port
    $baseUrl = "http://127.0.0.1:$port/$publicationName/hs/mcp-data/"

    $profilesDirectory = Join-Path $script:LocalRoot "profiles"
    New-Item -ItemType Directory -Force -Path $profilesDirectory | Out-Null
    $credentialPath = Join-Path $profilesDirectory "$profileId.credentials.bin"
    $cipher = Protect-Credentials $User $Password $profileId
    try { [IO.File]::WriteAllBytes($credentialPath, $cipher) } finally { [Array]::Clear($cipher, 0, $cipher.Length) }
    $profilePath = Join-Path $profilesDirectory "$profileId.json"
    $profile = [ordered]@{
        version = 1; id = $profileId; name = $ProfileName; base_url = $baseUrl
        mcp_executable = (Join-Path $binDirectory "mcp-1c-data.exe")
        credential_file = $credentialPath; timeout = "30s"; max_response_bytes = 4194304
    }
    [IO.File]::WriteAllText($profilePath, ($profile | ConvertTo-Json), (New-Object Text.UTF8Encoding($false)))
    Invoke-InfoSmokeTest $baseUrl $User $Password | Out-Null
    & (Join-Path $binDirectory "mcp-1c-data-launcher.exe") --profile $profilePath --check
    if ($LASTEXITCODE -ne 0) { throw "Лаунчер не смог проверить DPAPI-профиль." }

    $codexBackup = $null
    if ($UpdateCodex) {
        $configPath = Join-Path $env:USERPROFILE ".codex\config.toml"
        $codexBackup = Set-CodexManagedBlock $configPath $profileId (Join-Path $binDirectory "mcp-1c-data-launcher.exe") $profilePath
        Write-SafeLog "Конфигурация Codex обновлена; прежние MCP-профили сохранены."
    }
    Write-SafeLog "Установка завершена."
    return @{ ProfileId = $profileId; ProfilePath = $profilePath; BaseUrl = $baseUrl; Backup = $backup; CodexBackup = $codexBackup; ApacheConfig = $apache.ConfPath }
}

function Invoke-SelfTest {
    $testRoot = Join-Path ([IO.Path]::GetTempPath()) ("mcp1cdata-selftest-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $testRoot | Out-Null
    try {
        if ((ConvertTo-ProfileSlug "Тест База №1") -ne "test_baza_1") { throw "slug test failed" }
        $plain = '{"user":"test","password":"self-test-secret"}'
        $cipher = Protect-Credentials "test" "self-test-secret" "selftest"
        if ([Text.Encoding]::UTF8.GetString($cipher).Contains("self-test-secret")) { throw "DPAPI plaintext leak" }
        $roundTrip = [Text.Encoding]::UTF8.GetString((Unprotect-CredentialsForTest $cipher "selftest"))
        if ($roundTrip -ne $plain) { throw "DPAPI round-trip failed" }
        $config = Join-Path $testRoot "config.toml"
        [IO.File]::WriteAllText($config, "[mcp_servers.existing]`ncommand = `"old.exe`"`n")
        Set-CodexManagedBlock $config "demo" "C:\MCP\launcher.exe" "C:\MCP\demo.json" | Out-Null
        Set-CodexManagedBlock $config "demo" "C:\MCP\launcher.exe" "C:\MCP\demo.json" | Out-Null
        $updated = [IO.File]::ReadAllText($config)
        if (-not $updated.Contains("[mcp_servers.existing]") -or ([regex]::Matches($updated, "MCP1CData:demo >>>").Count -ne 1)) { throw "Codex managed-block test failed" }
        $port = Get-FreeLoopbackPort 8080
        if ($port -lt 8080 -or $port -gt 8099) { throw "port test failed" }
        $auth = New-DesignerAuthArguments "designer" "secret"
        if (($auth -join '|') -ne '/N|designer|/P|secret') { throw "Designer authentication test failed" }
        $payloadSource = Join-Path $testRoot "payload-source.exe"
        $payloadDestination = Join-Path $testRoot "payload-destination.exe"
        [IO.File]::WriteAllText($payloadSource, "identical-payload", (New-Object Text.UTF8Encoding($false)))
        [IO.File]::WriteAllText($payloadDestination, "identical-payload", (New-Object Text.UTF8Encoding($false)))
        $payloadTimestamp = [DateTime]::UtcNow.AddDays(-2)
        [IO.File]::SetLastWriteTimeUtc($payloadDestination, $payloadTimestamp)
        Install-PayloadFile $payloadSource $payloadDestination
        if ([IO.File]::GetLastWriteTimeUtc($payloadDestination) -ne $payloadTimestamp) { throw "identical payload was overwritten" }
        $apacheText = New-ApacheConfigurationText "Define SRVROOT `"c:/Apache24`"`nListen 80`n Listen 0.0.0.0:443" "c:/safe" "c:/logs" "c:/1c/wsap24.dll" "c:/pub" "c:/pub/default.vrd" "demo" 8080
        $activeListeners = [regex]::Matches($apacheText, '(?m)^\s*Listen\s+([^#\r\n]+)$')
        if ($activeListeners.Count -ne 1 -or $activeListeners[0].Groups[1].Value.Trim() -ne "127.0.0.1:8080") { throw "loopback-only Apache test failed" }
        $vrd = New-VrdText "C:\Base" "demo"
        if (-not $vrd.Contains('publishByDefault="false"') -or -not $vrd.Contains('publishExtensionsByDefault="true"') -or -not $vrd.Contains('service name="MCPDataService" rootUrl="mcp-data" enable="true"') -or -not $vrd.Contains('standardOdata enable="false"')) { throw "VRD allow-list test failed" }
        if (-not $apacheText.Contains('<Location "/demo">') -or -not $apacheText.Contains('<Location "/demo/hs/mcp-data">')) { throw "Apache path allow-list test failed" }
        if (-not $apacheText.Contains('PidFile "c:/logs/httpd-demo.pid"') -or -not $apacheText.Contains('ErrorLog "c:/logs/apache-error-demo.log"')) { throw "per-profile Apache runtime files test failed" }
        Write-Host "SELFTEST PASS: DPAPI, Codex config, slug, port, loopback-only Apache and VRD allow-list."
    } finally {
        if (Test-Path -LiteralPath $testRoot) {
            $resolved = [IO.Path]::GetFullPath($testRoot)
            $allowed = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
            if ($resolved.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path -Leaf $resolved) -like "mcp1cdata-selftest-*") {
                Remove-Item -LiteralPath $resolved -Recurse -Force
            }
        }
    }
}

function Resume-MCP1CDataProfile {
    param([string]$ProfilePath, [bool]$UpdateCodex)
    $resolvedProfile = (Resolve-Path -LiteralPath $ProfilePath).Path
    $profile = [IO.File]::ReadAllText($resolvedProfile, [Text.Encoding]::UTF8) | ConvertFrom-Json
    if ($profile.version -ne 1 -or [string]::IsNullOrWhiteSpace([string]$profile.id)) { throw "Профиль имеет неподдерживаемый формат." }
    $cipher = [IO.File]::ReadAllBytes([string]$profile.credential_file)
    $plain = Unprotect-CredentialsForTest $cipher ([string]$profile.id)
    try {
        $credentials = [Text.Encoding]::UTF8.GetString($plain) | ConvertFrom-Json
        $runId = Get-Date -Format "yyyyMMdd-HHmmss"
        $logDirectory = Join-Path $script:LocalRoot "logs\resume-$runId"
        New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
        $script:LogFile = Join-Path $logDirectory "install.log"
        Write-SafeLog "Продолжение установки профиля '$($profile.id)' после настройки публикации."
        Invoke-InfoSmokeTest ([string]$profile.base_url) ([string]$credentials.user) ([string]$credentials.password) | Out-Null
        $binDirectory = Split-Path -Parent ([string]$profile.mcp_executable)
        $launcher = Join-Path $binDirectory "mcp-1c-data-launcher.exe"
        & $launcher --profile $resolvedProfile --check
        if ($LASTEXITCODE -ne 0) { throw "Лаунчер не смог проверить DPAPI-профиль." }
        $codexBackup = $null
        if ($UpdateCodex) {
            $configPath = Join-Path $env:USERPROFILE ".codex\config.toml"
            $codexBackup = Set-CodexManagedBlock $configPath ([string]$profile.id) $launcher $resolvedProfile
            Write-SafeLog "Конфигурация Codex обновлена; прежние MCP-профили сохранены."
        }
        Write-SafeLog "Продолжение установки завершено успешно."
        return @{ ProfilePath = $resolvedProfile; BaseUrl = [string]$profile.base_url; CodexBackup = $codexBackup }
    } finally {
        if ($plain) { [Array]::Clear($plain, 0, $plain.Length) }
        if ($cipher) { [Array]::Clear($cipher, 0, $cipher.Length) }
        $credentials = $null
    }
}

if ($SelfTest) { Invoke-SelfTest; exit 0 }
if ($CheckEnvironment) {
    if ([string]::IsNullOrWhiteSpace($InfoBasePath)) { throw "Для -CheckEnvironment укажите -InfoBasePath." }
    Assert-InstallPreflight $InfoBasePath -SkipPayload | ConvertTo-Json
    exit 0
}
if (-not [string]::IsNullOrWhiteSpace($ResumeProfile)) {
    Resume-MCP1CDataProfile $ResumeProfile ([bool]$UpdateCodexOnResume) | ConvertTo-Json
    exit 0
}

Add-Type -AssemblyName PresentationFramework, PresentationCore, WindowsBase, System.Xaml
[xml]$xaml = @'
<Window xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation" xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml"
        Title="MCP для 1С — мастер установки" Width="760" Height="650" WindowStartupLocation="CenterScreen" ResizeMode="CanMinimize">
  <Grid Margin="22">
    <Grid.RowDefinitions><RowDefinition Height="Auto"/><RowDefinition Height="*"/><RowDefinition Height="Auto"/></Grid.RowDefinitions>
    <StackPanel Grid.Row="0" Margin="0,0,0,18">
      <TextBlock Text="MCP для безопасного доступа к данным 1С" FontSize="22" FontWeight="SemiBold"/>
      <TextBlock Text="Локальная установка · 1С 8.3.27.2214 · только чтение с правами пользователя" Foreground="#666" Margin="0,5,0,0"/>
    </StackPanel>
    <ScrollViewer Grid.Row="1" VerticalScrollBarVisibility="Auto">
      <StackPanel>
        <TextBlock Text="1. Файловая база" FontSize="16" FontWeight="SemiBold" Margin="0,0,0,8"/>
        <DockPanel><Button Name="BrowseButton" Content="Обзор…" DockPanel.Dock="Right" Width="90" Margin="8,0,0,0"/><TextBox Name="BasePathBox" Height="28" VerticalContentAlignment="Center"/></DockPanel>
        <TextBlock Text="Каталог должен содержать 1Cv8.1CD." Foreground="#666" Margin="0,5,0,8"/>
        <CheckBox Name="BackupCheck" IsChecked="True" Content="Создать DT-копию базы перед установкой (рекомендуется)" Margin="0,0,0,16"/>
        <TextBlock Text="2. Профиль и учетная запись 1С" FontSize="16" FontWeight="SemiBold" Margin="0,0,0,8"/>
        <Grid><Grid.ColumnDefinitions><ColumnDefinition Width="210"/><ColumnDefinition Width="*"/></Grid.ColumnDefinitions><Grid.RowDefinitions><RowDefinition Height="36"/><RowDefinition Height="36"/><RowDefinition Height="36"/><RowDefinition Height="36"/><RowDefinition Height="36"/><RowDefinition Height="36"/></Grid.RowDefinitions>
          <TextBlock Grid.Row="0" Text="Название профиля" VerticalAlignment="Center"/><TextBox Name="ProfileNameBox" Grid.Row="0" Grid.Column="1" Height="28" Text="Моя база 1С"/>
          <TextBlock Grid.Row="1" Text="Пользователь MCP (RLS)" VerticalAlignment="Center"/><TextBox Name="UserBox" Grid.Row="1" Grid.Column="1" Height="28"/>
          <TextBlock Grid.Row="2" Text="Пароль MCP" VerticalAlignment="Center"/><PasswordBox Name="PasswordBox" Grid.Row="2" Grid.Column="1" Height="28"/>
          <TextBlock Grid.Row="3" Text="Пользователь Конфигуратора" VerticalAlignment="Center"/><TextBox Name="InstallerUserBox" Grid.Row="3" Grid.Column="1" Height="28"/>
          <TextBlock Grid.Row="4" Text="Пароль Конфигуратора" VerticalAlignment="Center"/><PasswordBox Name="InstallerPasswordBox" Grid.Row="4" Grid.Column="1" Height="28"/>
          <TextBlock Grid.Row="5" Text="Начальный порт" VerticalAlignment="Center"/><TextBox Name="PortBox" Grid.Row="5" Grid.Column="1" Height="28" Text="8080"/>
        </Grid>
        <TextBlock Text="Учетная запись Конфигуратора нужна для DT-копии и установки CFE. Оставьте ее пустой, чтобы использовать учетную запись MCP. Пароль MCP шифруется DPAPI; пароли не попадают в Codex или журналы." Foreground="#666" TextWrapping="Wrap" Margin="0,5,0,16"/>
        <TextBlock Text="3. Codex" FontSize="16" FontWeight="SemiBold" Margin="0,0,0,8"/>
        <CheckBox Name="CodexCheck" IsChecked="True" Content="После подтверждения добавить отдельный MCP-профиль в ~/.codex/config.toml"/>
        <TextBlock Text="Исходный config.toml будет сохранен рядом с отметкой времени; существующие профили не заменяются." Foreground="#666" Margin="20,5,0,16"/>
        <TextBlock Text="Ход установки" FontSize="16" FontWeight="SemiBold" Margin="0,0,0,8"/>
        <TextBox Name="LogBox" Height="155" IsReadOnly="True" TextWrapping="Wrap" VerticalScrollBarVisibility="Auto" FontFamily="Consolas" FontSize="11"/>
      </StackPanel>
    </ScrollViewer>
    <DockPanel Grid.Row="2" Margin="0,18,0,0"><Button Name="InstallButton" Content="Установить" Width="130" Height="34" DockPanel.Dock="Right" IsDefault="True"/><Button Name="CloseButton" Content="Закрыть" Width="100" Height="34" DockPanel.Dock="Right" Margin="0,0,8,0" IsCancel="True"/></DockPanel>
  </Grid>
</Window>
'@
$reader = New-Object System.Xml.XmlNodeReader $xaml
$window = [Windows.Markup.XamlReader]::Load($reader)
$basePathBox = $window.FindName("BasePathBox"); $profileNameBox = $window.FindName("ProfileNameBox")
$userBox = $window.FindName("UserBox"); $passwordBox = $window.FindName("PasswordBox"); $portBox = $window.FindName("PortBox")
$installerUserBox = $window.FindName("InstallerUserBox"); $installerPasswordBox = $window.FindName("InstallerPasswordBox")
$backupCheck = $window.FindName("BackupCheck"); $codexCheck = $window.FindName("CodexCheck"); $logBox = $window.FindName("LogBox"); $installButton = $window.FindName("InstallButton")
$script:ProgressCallback = { param($line) $logBox.AppendText($line + [Environment]::NewLine); $logBox.ScrollToEnd(); $window.Dispatcher.Invoke([Action]{}, [Windows.Threading.DispatcherPriority]::Render) }
$window.FindName("BrowseButton").Add_Click({
    Add-Type -AssemblyName System.Windows.Forms
    $dialog = New-Object Windows.Forms.FolderBrowserDialog
    $dialog.Description = "Выберите каталог файловой базы 1С"
    if ($dialog.ShowDialog() -eq [Windows.Forms.DialogResult]::OK) { $basePathBox.Text = $dialog.SelectedPath }
})
$window.FindName("CloseButton").Add_Click({ $window.Close() })
$installButton.Add_Click({
    try {
        if ([string]::IsNullOrWhiteSpace($basePathBox.Text) -or [string]::IsNullOrWhiteSpace($profileNameBox.Text) -or [string]::IsNullOrWhiteSpace($userBox.Text)) { throw "Заполните путь к базе, название профиля и пользователя 1С." }
        $port = 0; if (-not [int]::TryParse($portBox.Text, [ref]$port) -or $port -lt 8080 -or $port -gt 8099) { throw "Начальный порт должен быть от 8080 до 8099." }
        $createBackup = [bool]$backupCheck.IsChecked
        if (-not $createBackup) {
            $backupAnswer = [Windows.MessageBox]::Show("DT-копия перед изменением базы создаваться не будет. Продолжайте только при наличии проверенной внешней резервной копии. Установить без DT-копии?", "Установка без резервной копии", [Windows.MessageBoxButton]::YesNo, [Windows.MessageBoxImage]::Warning)
            if ($backupAnswer -ne [Windows.MessageBoxResult]::Yes) { return }
        }
        if ($codexCheck.IsChecked) {
            $answer = [Windows.MessageBox]::Show("Мастер создаст резервную копию config.toml и добавит отдельный профиль MCP. Продолжить?", "Подтверждение изменения Codex", [Windows.MessageBoxButton]::YesNo, [Windows.MessageBoxImage]::Question)
            if ($answer -ne [Windows.MessageBoxResult]::Yes) { return }
        }
        $installButton.IsEnabled = $false; $logBox.Clear()
        $result = Install-MCP1CData $basePathBox.Text $profileNameBox.Text $userBox.Text $passwordBox.Password $installerUserBox.Text $installerPasswordBox.Password $port ([bool]$codexCheck.IsChecked) $createBackup
        $passwordBox.Clear()
        $installerPasswordBox.Clear()
        $backupResult = if ($result.Backup) { $result.Backup } else { "не создавалась по выбору пользователя" }
        [Windows.MessageBox]::Show("Установка завершена.`n`nПрофиль: $($result.ProfilePath)`nРезервная копия: $backupResult`n`nПерезапустите Codex.", "Готово", [Windows.MessageBoxButton]::OK, [Windows.MessageBoxImage]::Information) | Out-Null
    } catch {
        Write-SafeLog $_.Exception.Message "ERROR"
        [Windows.MessageBox]::Show($_.Exception.Message, "Установка остановлена", [Windows.MessageBoxButton]::OK, [Windows.MessageBoxImage]::Error) | Out-Null
    } finally {
        $passwordBox.Clear(); $installerPasswordBox.Clear(); $installButton.IsEnabled = $true
    }
})
[void]$window.ShowDialog()
