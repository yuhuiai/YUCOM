$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$releaseRoot = Join-Path $projectRoot 'release'
$versionName = 'YUCOM-V1.2.0-Windows'
$versionRoot = Join-Path $releaseRoot $versionName
$archivePath = Join-Path $releaseRoot ($versionName + '.zip')

if (Test-Path -LiteralPath $versionRoot) {
    $resolvedProject = [IO.Path]::GetFullPath($projectRoot)
    $resolvedTarget = [IO.Path]::GetFullPath($versionRoot)
    if (-not $resolvedTarget.StartsWith($resolvedProject + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw '拒绝清理项目目录外的发布文件。'
    }
    Remove-Item -LiteralPath $resolvedTarget -Recurse -Force
}
if (Test-Path -LiteralPath $archivePath) {
    Remove-Item -LiteralPath $archivePath -Force
}

New-Item -ItemType Directory -Path $versionRoot -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $projectRoot 'dist\YUCOM-Windows-amd64.exe') -Destination $versionRoot
Copy-Item -LiteralPath (Join-Path $projectRoot 'dist\YUCOM-Windows-arm64.exe') -Destination $versionRoot
Copy-Item -LiteralPath (Join-Path $projectRoot 'README.txt') -Destination $versionRoot
Copy-Item -LiteralPath (Join-Path $projectRoot '01-安装和使用步骤.txt') -Destination $versionRoot
Copy-Item -LiteralPath (Join-Path $projectRoot '02-通用串口测试步骤.txt') -Destination $versionRoot

$manifestLines = Get-ChildItem -LiteralPath $versionRoot -File |
    Sort-Object Name |
    ForEach-Object {
        $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash *$($_.Name)"
    }
[IO.File]::WriteAllLines((Join-Path $versionRoot 'SHA256SUMS.txt'), $manifestLines, [Text.UTF8Encoding]::new($false))

Compress-Archive -LiteralPath $versionRoot -DestinationPath $archivePath -CompressionLevel Optimal
Write-Host "Release created: $archivePath"
