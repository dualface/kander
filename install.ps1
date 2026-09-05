$ErrorActionPreference = "Stop"

$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8

function Write-Stderr {
  param([string]$Message)
  [Console]::Error.WriteLine($Message)
}

function Get-LexicalEntry {
  param([string]$Path)

  $parent = Split-Path -Parent $Path
  $leaf = Split-Path -Leaf $Path
  if (-not [IO.Directory]::Exists($parent)) {
    return $null
  }
  return @(
    Get-ChildItem -LiteralPath $parent -Force -ErrorAction Stop |
      Where-Object { $_.Name -ieq $leaf }
  ) | Select-Object -First 1
}

function Test-ReparsePoint {
  param($Item)
  return ($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
}

function Show-Usage {
  param([bool]$Chinese, [bool]$ErrorStream)

  if ($Chinese) {
    $lines = @(
      "用法: install.ps1 [--lang {cn,en}] [--project <目录>]",
      "把 kander 装到 ~/.local/bin, 规则装到 ~/.agents, 资源装到 ~/.local/share/kander.",
      "指定 --project 时只装到该 Git 项目主 worktree 的 .kander/, 不写全局路径."
    )
  } else {
    $lines = @(
      "usage: install.ps1 [--lang {cn,en}] [--project <directory>]",
      "Install kander to ~/.local/bin, rules to ~/.agents, and assets to ~/.local/share/kander.",
      "With --project, install only into that Git project's main worktree .kander/ and skip global paths."
    )
  }
  foreach ($line in $lines) {
    if ($ErrorStream) {
      Write-Stderr $line
    } else {
      [Console]::Out.WriteLine($line)
    }
  }
}

function Fail-Install {
  param([string]$Message)
  throw [InvalidOperationException]::new($Message)
}

function Assert-DirectoryTarget {
  param([string]$Path, [bool]$Chinese)

  $item = Get-LexicalEntry $Path
  if ($null -eq $item) {
    return
  }
  if (-not $item.PSIsContainer) {
    if ($Chinese) {
      Fail-Install "错误: 安装目标不是目录: $Path"
    } else {
      Fail-Install "error: installation target is not a directory: $Path"
    }
  }
  if (Test-ReparsePoint $item) {
    if ($Chinese) {
      Fail-Install "错误: 安装目录不得为重解析点: $Path"
    } else {
      Fail-Install "error: installation directory must not be a reparse point: $Path"
    }
  }
}

function Assert-FileTarget {
  param([string]$Path, [bool]$Chinese, [bool]$Legacy)

  $item = Get-LexicalEntry $Path
  if ($null -eq $item) {
    return
  }
  if ($item.PSIsContainer) {
    if ($Legacy -and $Chinese) {
      Fail-Install "错误: 旧版安装目标是目录: $Path"
    } elseif ($Legacy) {
      Fail-Install "error: legacy installation target is a directory: $Path"
    } elseif ($Chinese) {
      Fail-Install "错误: 安装目标是目录: $Path"
    } else {
      Fail-Install "error: installation target is a directory: $Path"
    }
  }
  if (Test-ReparsePoint $item) {
    if ($Chinese) {
      Fail-Install "错误: 安装文件目标不得为重解析点: $Path"
    } else {
      Fail-Install "error: installation file target must not be a reparse point: $Path"
    }
  }
}

function Get-SourceFiles {
  param([string]$Directory, [string]$Extension)

  if (-not [IO.Directory]::Exists($Directory)) {
    return @()
  }
  $files = @(
    Get-ChildItem -LiteralPath $Directory -Force -File -ErrorAction Stop |
      Where-Object { [string]::IsNullOrEmpty($Extension) -or $_.Extension -ieq $Extension } |
      Sort-Object Name
  )
  return $files
}

function Get-ShareFiles {
  param([string]$Directory)

  if (-not [IO.Directory]::Exists($Directory)) {
    return @()
  }
  return @(
    Get-ChildItem -LiteralPath $Directory -Force -File -Recurse -ErrorAction Stop |
      Sort-Object FullName
  )
}

function Test-PathEntryExists {
  param([string]$Path)
  return $null -ne (Get-LexicalEntry $Path)
}

function Invoke-Git {
  param([string[]]$GitArgs)

  $previous = @{
    GIT_DIR = [Environment]::GetEnvironmentVariable("GIT_DIR", "Process")
    GIT_WORK_TREE = [Environment]::GetEnvironmentVariable("GIT_WORK_TREE", "Process")
    GIT_COMMON_DIR = [Environment]::GetEnvironmentVariable("GIT_COMMON_DIR", "Process")
  }
  try {
    foreach ($name in @("GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR")) {
      [Environment]::SetEnvironmentVariable($name, $null, "Process")
    }
    $output = & git @GitArgs 2>$null
    return [PSCustomObject]@{
      ExitCode = $LASTEXITCODE
      Output = @($output)
    }
  } finally {
    foreach ($name in $previous.Keys) {
      [Environment]::SetEnvironmentVariable($name, $previous[$name], "Process")
    }
  }
}

function Get-MainWorktree {
  param([string]$Directory, [bool]$Chinese)

  $item = Get-LexicalEntry $Directory
  if ($null -eq $item) {
    if ($Chinese) {
      Fail-Install "错误: 项目目录不存在: $Directory"
    } else {
      Fail-Install "error: project directory does not exist: $Directory"
    }
  }
  if (Test-ReparsePoint $item) {
    if ($Chinese) {
      Fail-Install "错误: 路径分量不得是符号链接/重解析点: $Directory"
    } else {
      Fail-Install "error: path component must not be a symlink/reparse point: $Directory"
    }
  }
  if (-not $item.PSIsContainer) {
    if ($Chinese) {
      Fail-Install "错误: 项目目录不存在: $Directory"
    } else {
      Fail-Install "error: project directory does not exist: $Directory"
    }
  }
  $absolute = [IO.Path]::GetFullPath($Directory)
  $listed = Invoke-Git @("-C", $absolute, "worktree", "list", "--porcelain")
  if ($listed.ExitCode -ne 0) {
    if ($Chinese) {
      Fail-Install "错误: 项目不是 Git 仓库: $absolute"
    } else {
      Fail-Install "error: project is not a Git repository: $absolute"
    }
  }
  $main = ""
  foreach ($line in $listed.Output) {
    if ([string]$line -like "worktree *") {
      $main = ([string]$line).Substring(9)
      break
    }
  }
  if ([string]::IsNullOrWhiteSpace($main)) {
    if ($Chinese) {
      Fail-Install "错误: 项目不是 Git 仓库: $absolute"
    } else {
      Fail-Install "error: project is not a Git repository: $absolute"
    }
  }
  return [IO.Path]::GetFullPath($main)
}

function Add-GitExclude {
  param([string]$GitRoot, [bool]$Chinese)

  Assert-DirectoryTarget $GitRoot $Chinese
  $resolved = Invoke-Git @("-C", $GitRoot, "rev-parse", "--git-path", "info/exclude")
  if ($resolved.ExitCode -ne 0 -or $resolved.Output.Count -eq 0) {
    if ($Chinese) {
      Fail-Install "错误: 无法定位 Git info/exclude"
    } else {
      Fail-Install "error: Cannot locate Git info/exclude"
    }
  }
  $exclude = [string]$resolved.Output[0]
  if (-not [IO.Path]::IsPathRooted($exclude)) {
    $exclude = Join-Path $GitRoot $exclude
  }
  $exclude = [IO.Path]::GetFullPath($exclude)
  $parent = Split-Path -Parent $exclude
  New-Item -ItemType Directory -Path $parent -Force | Out-Null
  Assert-DirectoryTarget $parent $Chinese
  Assert-FileTarget $exclude $Chinese $false
  $pattern = "/.kander/"
  if ([IO.File]::Exists($exclude)) {
    $text = [IO.File]::ReadAllText($exclude)
    $lines = $text -split "\r?\n"
    if ($lines -contains $pattern) {
      return
    }
    if ($text.Length -gt 0 -and -not $text.EndsWith("`n")) {
      [IO.File]::AppendAllText($exclude, "`r`n$pattern`r`n")
    } else {
      [IO.File]::AppendAllText($exclude, "$pattern`r`n")
    }
  } else {
    [IO.File]::WriteAllText($exclude, "$pattern`r`n")
  }
}

function Get-BuiltBinary {
  param([string]$ProjectDir, [bool]$Chinese)

  $candidate = Join-Path $ProjectDir "kander.exe"
  $item = Get-LexicalEntry $candidate
  if ($null -ne $item) {
    Assert-FileTarget $candidate $Chinese $false
    if (-not $item.PSIsContainer) {
      return $candidate
    }
  }
  $alt = Join-Path $ProjectDir "kander"
  $altItem = Get-LexicalEntry $alt
  if ($null -ne $altItem -and -not $altItem.PSIsContainer) {
    Assert-FileTarget $alt $Chinese $false
    return $alt
  }
  $go = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
  if ($null -eq $go) {
    if ($Chinese) {
      Fail-Install "错误: 未找到已构建的 kander, 且 go 不可用"
    } else {
      Fail-Install "error: built kander is missing and go is unavailable"
    }
  }
  Push-Location -LiteralPath $ProjectDir
  try {
    $buildTimestamp = [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ")
    $gitHash = "unknown"
    try {
      $revision = Invoke-Git @("-C", $ProjectDir, "rev-parse", "--short=12", "HEAD")
      if ($revision.ExitCode -eq 0 -and $revision.Output.Count -gt 0) {
        $candidateHash = [string]$revision.Output[0]
        if (-not [string]::IsNullOrWhiteSpace($candidateHash)) {
          $gitHash = $candidateHash.Trim()
        }
      }
    } catch {
      $gitHash = "unknown"
    }
    $versionPackage = "github.com/dualface/kander/internal/version"
    $versionLdflags = "-X $versionPackage.BuildTimestamp=$buildTimestamp -X $versionPackage.GitHash=$gitHash"
    & $go.Source build -ldflags $versionLdflags -o kander.exe ./cmd/kander
    if ($LASTEXITCODE -ne 0) {
      if ($Chinese) {
        Fail-Install "错误: go build 失败"
      } else {
        Fail-Install "error: go build failed"
      }
    }
  } finally {
    Pop-Location
  }
  return (Join-Path $ProjectDir "kander.exe")
}

$installArgs = @($args)
$languageSet = $false
$requestedLanguage = ""
$projectSet = $false
$projectArg = ""
$showHelp = $false
$parseError = $false
$missingLanguageValue = $false
$missingProject = $false
$duplicateProject = $false
$index = 0

while ($index -lt $installArgs.Count) {
  $current = [string]$installArgs[$index]
  if ($current -eq "--lang") {
    $languageSet = $true
    if ($index + 1 -ge $installArgs.Count) {
      $missingLanguageValue = $true
      $parseError = $true
      break
    }
    $requestedLanguage = [string]$installArgs[$index + 1]
    $index += 2
    continue
  }
  if ($current -like "--lang=*") {
    $languageSet = $true
    $requestedLanguage = $current.Substring(7)
    $index += 1
    continue
  }
  if ($current -eq "--project") {
    if ($projectSet) {
      $duplicateProject = $true
      $parseError = $true
      break
    }
    if ($index + 1 -ge $installArgs.Count) {
      $missingProject = $true
      $parseError = $true
      break
    }
    $next = [string]$installArgs[$index + 1]
    if ($next -in @("--lang", "--project", "-h", "--help") -or $next.StartsWith("--lang=") -or $next.StartsWith("--project=")) {
      $missingProject = $true
      $parseError = $true
      break
    }
    $projectSet = $true
    $projectArg = $next
    $index += 2
    continue
  }
  if ($current -like "--project=*") {
    if ($projectSet) {
      $duplicateProject = $true
      $parseError = $true
      break
    }
    $projectSet = $true
    $projectArg = $current.Substring(10)
    $index += 1
    continue
  }
  if ($current -in @("-h", "--help")) {
    $showHelp = $true
    $index += 1
    continue
  }
  $parseError = $true
  break
}

if ($projectSet -and [string]::IsNullOrEmpty($projectArg)) {
  $missingProject = $true
  $parseError = $true
}

$projectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$locale = ""
if ($requestedLanguage -in @("cn", "en")) {
  $locale = $requestedLanguage
}
if ([string]::IsNullOrEmpty($locale)) {
  foreach ($name in @("KANDER_LANG", "LC_ALL", "LC_MESSAGES", "LANG")) {
    $value = [Environment]::GetEnvironmentVariable($name)
    if (-not [string]::IsNullOrEmpty($value)) {
      $locale = $value
      break
    }
  }
}
$chinese = -not ([string]$locale -match "^(?i:en)")

if ($missingLanguageValue -or ($languageSet -and $requestedLanguage -notin @("cn", "en"))) {
  Show-Usage $chinese $true
  if ($chinese) {
    Write-Stderr "错误: --lang 只接受 cn 或 en"
  } else {
    Write-Stderr "error: --lang must be cn or en"
  }
  exit 2
}
if ($missingProject) {
  Show-Usage $chinese $true
  if ($chinese) {
    Write-Stderr "错误: --project 需要目录"
  } else {
    Write-Stderr "error: --project requires a directory"
  }
  exit 2
}
if ($duplicateProject) {
  Show-Usage $chinese $true
  if ($chinese) {
    Write-Stderr "错误: --project 只能指定一次"
  } else {
    Write-Stderr "error: --project may be given only once"
  }
  exit 2
}
if ($showHelp -and -not $parseError) {
  Show-Usage $chinese $false
  exit 0
}
if ($parseError) {
  Show-Usage $chinese $true
  exit 2
}

try {
  $homeValue = [Environment]::GetEnvironmentVariable("USERPROFILE")
  if ([string]::IsNullOrWhiteSpace($homeValue)) {
    $homeValue = [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
  }
  if ([string]::IsNullOrWhiteSpace($homeValue)) {
    if ($chinese) {
      Fail-Install "错误: 无法确定用户主目录"
    } else {
      Fail-Install "error: could not determine the user home directory"
    }
  }
  $userHome = [IO.Path]::GetFullPath($homeValue)

  $rulesSource = Join-Path $projectDir "rules"
  $shareSource = Join-Path $projectDir "share"
  $ruleFiles = @(Get-SourceFiles $rulesSource ".md")
  $shareFiles = @(Get-ShareFiles $shareSource)
  $binary = Get-BuiltBinary $projectDir $chinese
  $binaryName = "kander.exe"

  function Install-Payloads {
    param([string]$DestBin, [string]$DestAgents, [string]$DestShare)

    New-Item -ItemType Directory -Path $DestBin -Force | Out-Null
    New-Item -ItemType Directory -Path $DestAgents -Force | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $DestBin $binaryName) -Force
    foreach ($source in $ruleFiles) {
      Copy-Item -LiteralPath $source.FullName -Destination (Join-Path $DestAgents $source.Name) -Force
    }
    foreach ($source in $shareFiles) {
      $relative = $source.FullName.Substring($shareSource.Length).TrimStart("\", "/")
      $destination = Join-Path $DestShare $relative
      New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
      Copy-Item -LiteralPath $source.FullName -Destination $destination -Force
    }
    $agentRules = Join-Path $DestAgents "AGENTS.md"
    $entryRules = Join-Path $DestAgents "KANDER-AGENTS.md"
    if ([IO.File]::Exists($entryRules) -and -not (Test-PathEntryExists $agentRules)) {
      $linked = $false
      try {
        New-Item -ItemType HardLink -Path $agentRules -Target $entryRules -ErrorAction Stop | Out-Null
        $linked = $true
      } catch {
        try {
          New-Item -ItemType SymbolicLink -Path $agentRules -Target $entryRules -ErrorAction Stop | Out-Null
          $linked = $true
        } catch {
          $linked = $false
        }
      }
      if (-not $linked) {
        if ($chinese) {
          Fail-Install "错误: 无法安全创建 $agentRules; 文件系统需支持硬链接或符号链接"
        } else {
          Fail-Install "error: could not safely create $agentRules; the file system must support hard links or symbolic links"
        }
      }
    }
  }

  function Assert-PayloadTargets {
    param([string]$DestBin, [string]$DestAgents, [string]$DestShare)

    Assert-FileTarget (Join-Path $DestBin $binaryName) $chinese $false
    foreach ($source in $ruleFiles) {
      Assert-FileTarget (Join-Path $DestAgents $source.Name) $chinese $false
    }
    if ($shareFiles.Count -gt 0) {
      Assert-DirectoryTarget $DestShare $chinese
      foreach ($source in $shareFiles) {
        $relative = $source.FullName.Substring($shareSource.Length).TrimStart("\", "/")
        $current = $DestShare
        $parts = @($relative -split '[\\/]' | Where-Object { $_ -ne "" })
        if ($parts.Count -eq 0) {
          continue
        }
        for ($i = 0; $i -lt $parts.Count - 1; $i++) {
          $current = Join-Path $current $parts[$i]
          Assert-DirectoryTarget $current $chinese
        }
        Assert-FileTarget (Join-Path $DestShare $relative) $chinese $false
      }
    }
  }

  if ($projectSet) {
    $main = Get-MainWorktree $projectArg $chinese
    $installRoot = Join-Path $main ".kander"
    $destBin = Join-Path $installRoot "bin"
    $destAgents = Join-Path $installRoot "rules"
    $destShare = Join-Path $installRoot "share"
    foreach ($directory in @($installRoot, $destBin, $destAgents, $destShare)) {
      Assert-DirectoryTarget $directory $chinese
    }
    Assert-PayloadTargets $destBin $destAgents $destShare
    Add-GitExclude $main $chinese
    Install-Payloads $destBin $destAgents $destShare
    if ($chinese) {
      [Console]::Out.WriteLine("Kander 已安装")
    } else {
      [Console]::Out.WriteLine("Kander installed")
    }
    [Console]::Out.WriteLine((Join-Path $destBin $binaryName))
    if ($chinese) {
      Write-Stderr "项目安装完成, 未修改 PATH, 也未改动全局 Kander 安装."
      Write-Stderr "请使用以上绝对路径."
    } else {
      Write-Stderr "Project install finished; PATH and the global Kander install were not changed."
      Write-Stderr "Use the absolute command path above."
    }
    exit 0
  }

  $binDir = Join-Path $userHome ".local\bin"
  $agentsDir = Join-Path $userHome ".agents"
  $shareDir = Join-Path $userHome ".local\share\kander"

  $directoryTargets = @(
    $userHome,
    (Join-Path $userHome ".local"),
    $binDir,
    $agentsDir
  )
  if ($shareFiles.Count -gt 0) {
    $directoryTargets += @(
      (Join-Path $userHome ".local\share"),
      $shareDir
    )
  }
  foreach ($directory in $directoryTargets | Select-Object -Unique) {
    Assert-DirectoryTarget $directory $chinese
  }
  Assert-PayloadTargets $binDir $agentsDir $shareDir

  $legacyNames = @(
    "onevoke",
    "onevoke.exe",
    "onevoke.cmd",
    "kanban",
    "kanban.exe",
    "kanban.cmd",
    "onevoke-review",
    "onevoke-review.exe",
    "onevoke-review.cmd",
    "onevoke-review.sh"
  )
  $legacyFound = @()
  foreach ($name in $legacyNames) {
    $target = Join-Path $binDir $name
    Assert-FileTarget $target $chinese $true
    if (Test-PathEntryExists $target) {
      $legacyFound += $name
    }
  }

  $removeLegacy = $false
  if ($legacyFound.Count -gt 0) {
    if ($chinese) {
      Write-Stderr "检测到已退役的 onevoke/kanban 入口:"
      Write-Stderr ("  " + ($legacyFound -join " "))
      Write-Stderr "命令入口现已统一为 kander."
      [Console]::Error.Write("是否删除这些旧入口? [y/N] ")
    } else {
      Write-Stderr "Retired onevoke/kanban entries were detected:"
      Write-Stderr ("  " + ($legacyFound -join " "))
      Write-Stderr "The command entry point is now unified as kander."
      [Console]::Error.Write("Delete these legacy entries? [y/N] ")
    }
    $legacyAnswer = [Console]::In.ReadLine()
    if ([Console]::IsInputRedirected) {
      [Console]::Error.WriteLine()
    }
    if ($legacyAnswer -in @("y", "Y", "yes", "YES", "Yes", "是")) {
      $removeLegacy = $true
    } elseif ($chinese) {
      Write-Stderr "已保留旧入口."
    } else {
      Write-Stderr "Legacy entries were kept."
    }
  }

  Install-Payloads $binDir $agentsDir $shareDir

  if ($removeLegacy) {
    $entry = Join-Path $binDir $binaryName
    $entryItem = Get-LexicalEntry $entry
    if ($null -eq $entryItem -or $entryItem.PSIsContainer -or (Test-ReparsePoint $entryItem)) {
      if ($chinese) {
        Fail-Install "错误: 新入口不可用, 已保留旧入口: $entry"
      } else {
        Fail-Install "error: the new entry is unavailable; legacy entries were kept: $entry"
      }
    }
    foreach ($name in $legacyFound) {
      [IO.File]::Delete((Join-Path $binDir $name))
    }
    if ($chinese) {
      Write-Stderr "已删除旧入口."
    } else {
      Write-Stderr "Legacy entries were removed."
    }
  }

  $pathEntries = @($env:PATH -split ";" | ForEach-Object { $_.Trim().Trim('"').TrimEnd("\") })
  if (-not ($pathEntries -contains $binDir.TrimEnd("\"))) {
    if ($chinese) {
      Write-Stderr "提示: $binDir 不在 PATH 中. 安装器不会自动修改用户 PATH; 请手动添加并重新打开终端."
    } else {
      Write-Stderr "note: $binDir is not on PATH. The installer does not modify the user PATH; add it manually and reopen the terminal."
    }
  }

  if ($chinese) {
    [Console]::Out.WriteLine("Kander 已安装")
  } else {
    [Console]::Out.WriteLine("Kander installed")
  }

  exit 0
} catch {
  Write-Stderr ([string]$_.Exception.Message)
  exit 1
}
