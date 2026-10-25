param(
    [string]$Version = '26.05.29',
    [string]$Output = '',
    [ValidateSet('builtin', 'ffmpeg')]
    [string]$MediaBackend = 'ffmpeg',
    [switch]$NativeVocallocal
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$pngPath = Join-Path $repoRoot 'public/images/favicon.png'
$icoPath = Join-Path $repoRoot 'build/windows/weatherhds.ico'
$manifestPath = Join-Path $repoRoot 'build/windows/app.manifest'
$versionInfoPath = Join-Path $repoRoot 'build/windows/versioninfo.json'
$resourcePath = Join-Path $repoRoot 'resource_windows_amd64.syso'
function Get-PortableOSName {
    if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) {
        return 'Windows'
    }
    if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Linux)) {
        return 'Linux'
    }
    if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)) {
        return 'macOS'
    }
    return 'UnknownOS'
}

function Get-PortableArchName {
    switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
        'X64' { return 'x86_64' }
        'Arm64' { return 'aarch64' }
        'X86' { return 'x86' }
        'Arm' { return 'armv7' }
        default { return [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant() }
    }
}

function Get-DefaultOutputPath {
    return "dist/WeatherHDS-$(Get-PortableOSName)-$(Get-PortableArchName)-Portable/WeatherHDS Server.exe"
}

if ([string]::IsNullOrWhiteSpace($Output)) {
    $Output = Get-DefaultOutputPath
}

$distRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot 'dist'))
$outputPath = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $Output))
$outputDir = Split-Path -Parent $outputPath
$distRootWithSeparator = $distRoot.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar

if ($outputPath -ne $distRoot -and -not $outputPath.StartsWith($distRootWithSeparator, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to write outside the dist directory: $outputPath"
}

function Get-MsysRoot {
    if ([string]::IsNullOrWhiteSpace($env:MSYS2_ROOT)) {
        return 'C:\msys64'
    }
    return $env:MSYS2_ROOT
}

function Initialize-Clang64BuildEnvironment {
    $msysRoot = Get-MsysRoot
    $msysUsrBin = Join-Path $msysRoot 'usr\bin'
    $clang64Root = Join-Path $msysRoot 'clang64'
    $clang64Bin = Join-Path $clang64Root 'bin'
    $clang64Lib = Join-Path $clang64Root 'lib'

    foreach ($requiredTool in @('x86_64-w64-mingw32-clang.exe', 'x86_64-w64-mingw32-clang++.exe', 'pkg-config.exe', 'llvm-objdump.exe')) {
        $requiredPath = Join-Path $clang64Bin $requiredTool
        if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
            throw "required MSYS2 CLANG64 tool not found: $requiredPath"
        }
    }

    $env:Path = "$clang64Bin;$msysUsrBin;$env:Path"
    $env:CGO_ENABLED = '1'
    $env:CC = Join-Path $clang64Bin 'x86_64-w64-mingw32-clang.exe'
    $env:CXX = Join-Path $clang64Bin 'x86_64-w64-mingw32-clang++.exe'
    $env:PKG_CONFIG = Join-Path $clang64Bin 'pkg-config.exe'
    $env:PKG_CONFIG_PATH = "$(Join-Path $clang64Lib 'pkgconfig');$(Join-Path $clang64Root 'share\pkgconfig')"
}

function Initialize-FfmpegBuildEnvironment {
    Initialize-Clang64BuildEnvironment

    $msysRoot = Get-MsysRoot
    $clang64Root = Join-Path $msysRoot 'clang64'
    $clang64Bin = Join-Path $clang64Root 'bin'
    $clang64Include = Join-Path $clang64Root 'include'
    $clang64Lib = Join-Path $clang64Root 'lib'
    foreach ($requiredPath in @(
        (Join-Path $clang64Include 'libavcodec\avcodec.h'),
        (Join-Path $clang64Include 'libavformat\avformat.h'),
        (Join-Path $clang64Include 'libswresample\swresample.h'),
        (Join-Path $clang64Lib 'pkgconfig\libavcodec.pc'),
        (Join-Path $clang64Lib 'pkgconfig\libavformat.pc'),
        (Join-Path $clang64Lib 'pkgconfig\libswresample.pc'),
        (Join-Path $clang64Bin 'avcodec-62.dll'),
        (Join-Path $clang64Bin 'avformat-62.dll'),
        (Join-Path $clang64Bin 'avutil-60.dll'),
        (Join-Path $clang64Bin 'swresample-6.dll')
    )) {
        if (-not (Test-Path -LiteralPath $requiredPath)) {
            throw "required MSYS2 CLANG64 FFmpeg file not found: $requiredPath"
        }
    }
}

function Get-PeImportedDllNames {
    param(
        [Parameter(Mandatory = $true)][string]$Path
    )

    $objdump = Get-Command 'llvm-objdump.exe' -ErrorAction SilentlyContinue
    if ($null -eq $objdump) {
        return @()
    }

    $output = & $objdump.Source -p $Path 2>$null
    $names = @()
    foreach ($line in $output) {
        if ($line -cmatch '^\s*DLL Name:\s*(.+)$') {
            $names += $Matches[1].Trim()
        }
    }
    return $names
}

function Copy-Clang64RuntimeDependencies {
    param(
        [Parameter(Mandatory = $true)][string[]]$EntryPoints,
        [Parameter(Mandatory = $true)][string]$DestinationDir
    )

    $msysRoot = Get-MsysRoot
    $clang64Bin = Join-Path (Join-Path $msysRoot 'clang64') 'bin'
    if (-not (Test-Path -LiteralPath $clang64Bin -PathType Container)) {
        return
    }

    New-Item -ItemType Directory -Force -Path $DestinationDir | Out-Null
    $queue = [System.Collections.Generic.Queue[string]]::new()
    $seen = @{}
    foreach ($entryPoint in $EntryPoints) {
        if (Test-Path -LiteralPath $entryPoint -PathType Leaf) {
            $fullEntryPoint = [System.IO.Path]::GetFullPath($entryPoint)
            if (-not $seen.ContainsKey($fullEntryPoint)) {
                $seen[$fullEntryPoint] = $true
                $queue.Enqueue($fullEntryPoint)
            }
        }
    }

    while ($queue.Count -gt 0) {
        $current = $queue.Dequeue()
        foreach ($dllName in (Get-PeImportedDllNames -Path $current)) {
            $source = Join-Path $clang64Bin $dllName
            if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
                continue
            }
            Copy-Item -LiteralPath $source -Destination $DestinationDir -Force
            $fullSource = [System.IO.Path]::GetFullPath($source)
            if (-not $seen.ContainsKey($fullSource)) {
                $seen[$fullSource] = $true
                $queue.Enqueue($fullSource)
            }
        }
    }
}

function Copy-SherpaOnnxRuntimeLibraries {
    param(
        [Parameter(Mandatory = $true)][string]$DestinationDir
    )

    $goOS = (go env GOOS 2>$null).Trim()
    $goArch = (go env GOARCH 2>$null).Trim()
    if ([string]::IsNullOrWhiteSpace($goOS) -or [string]::IsNullOrWhiteSpace($goArch)) {
        return
    }

    $module = switch ($goOS) {
        'windows' { 'github.com/k2-fsa/sherpa-onnx-go-windows' }
        'linux' { 'github.com/k2-fsa/sherpa-onnx-go-linux' }
        'darwin' { 'github.com/k2-fsa/sherpa-onnx-go-macos' }
        default { '' }
    }
    if ([string]::IsNullOrWhiteSpace($module)) {
        return
    }

    $triple = switch ("$goOS/$goArch") {
        'windows/amd64' { 'x86_64-pc-windows-gnu' }
        'windows/386' { 'i686-pc-windows-gnu' }
        'linux/amd64' { 'x86_64-unknown-linux-gnu' }
        'linux/arm64' { 'aarch64-unknown-linux-gnu' }
        'linux/arm' { 'arm-unknown-linux-gnueabihf' }
        'darwin/amd64' { 'x86_64-apple-darwin' }
        'darwin/arm64' { 'aarch64-apple-darwin' }
        default { '' }
    }
    if ([string]::IsNullOrWhiteSpace($triple)) {
        return
    }

    $moduleDir = (go list -m -f '{{.Dir}}' $module 2>$null).Trim()
    if ([string]::IsNullOrWhiteSpace($moduleDir)) {
        return
    }
    $libDir = Join-Path $moduleDir (Join-Path 'lib' $triple)
    if (-not (Test-Path -LiteralPath $libDir -PathType Container)) {
        return
    }

    Get-ChildItem -LiteralPath $libDir -File | Where-Object {
        $_.Extension -in @('.dll', '.so', '.dylib') -or $_.Name -like '*.so.*'
    } | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination $DestinationDir -Force
    }
}

function Convert-PngToIco {
    param(
        [Parameter(Mandatory = $true)][string]$Png,
        [Parameter(Mandatory = $true)][string]$Ico
    )

    $pngBytes = [System.IO.File]::ReadAllBytes($Png)
    if ($pngBytes.Length -lt 24) {
        throw 'Invalid PNG file.'
    }

    $pngSignature = [byte[]](137,80,78,71,13,10,26,10)
    for ($i = 0; $i -lt $pngSignature.Length; $i++) {
        if ($pngBytes[$i] -ne $pngSignature[$i]) {
            throw 'Input file is not a PNG image.'
        }
    }

    $width = ([uint32]$pngBytes[16] -shl 24) -bor ([uint32]$pngBytes[17] -shl 16) -bor ([uint32]$pngBytes[18] -shl 8) -bor [uint32]$pngBytes[19]
    $height = ([uint32]$pngBytes[20] -shl 24) -bor ([uint32]$pngBytes[21] -shl 16) -bor ([uint32]$pngBytes[22] -shl 8) -bor [uint32]$pngBytes[23]

    $widthByte = if ($width -ge 256) { [byte]0 } else { [byte]$width }
    $heightByte = if ($height -ge 256) { [byte]0 } else { [byte]$height }

    $stream = New-Object System.IO.MemoryStream
    $writer = New-Object System.IO.BinaryWriter($stream)
    $writer.Write([uint16]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]1)
    $writer.Write([byte]$widthByte)
    $writer.Write([byte]$heightByte)
    $writer.Write([byte]0)
    $writer.Write([byte]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]32)
    $writer.Write([uint32]$pngBytes.Length)
    $writer.Write([uint32]22)
    $writer.Write($pngBytes)
    [System.IO.File]::WriteAllBytes($Ico, $stream.ToArray())
    $writer.Dispose()
    $stream.Dispose()
}

if (-not (Test-Path -LiteralPath $pngPath)) {
    throw "Favicon PNG was not found at $pngPath"
}

New-Item -ItemType Directory -Path (Split-Path -Parent $icoPath) -Force | Out-Null
New-Item -ItemType Directory -Path $outputDir -Force | Out-Null

Convert-PngToIco -Png $pngPath -Ico $icoPath

go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest `
    -64 `
    -o $resourcePath `
    -manifest $manifestPath `
    -icon $icoPath `
    -product-name 'WeatherHDS Server' `
    -description 'WeatherHDS Server' `
    -company 'METEOchannel / meowraii' `
    -copyright 'Copyright (c) meowraii' `
    -internal-name 'weatherhds' `
    -original-name 'WeatherHDS Server.exe' `
    -file-version $Version `
    -product-version $Version `
    -propagate-ver-strings `
    $versionInfoPath

if ($LASTEXITCODE -ne 0) {
    throw 'goversioninfo resource generation failed.'
}

Push-Location $repoRoot
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $buildArgs = @('build', '-trimpath', '-ldflags', '-s -w', '-o', $outputPath)
    $buildTags = @()
    if ($MediaBackend -eq 'ffmpeg') {
        Initialize-FfmpegBuildEnvironment
        $buildTags += 'ffmpeg'
    } elseif ($NativeVocallocal) {
        Initialize-Clang64BuildEnvironment
    } else {
        $env:CGO_ENABLED = '0'
    }
    if ($NativeVocallocal) {
        $buildTags += 'native_tts'
    }
    if ($buildTags.Count -gt 0) {
        $buildArgs += @('-tags', ($buildTags -join ','))
    }
    $buildArgs += '.'
    go @buildArgs
    if ($LASTEXITCODE -ne 0) {
        throw 'go build failed.'
    }
    if ($MediaBackend -eq 'ffmpeg' -or $NativeVocallocal) {
        Copy-Clang64RuntimeDependencies -EntryPoints @($outputPath) -DestinationDir $outputDir
    }
    if ($NativeVocallocal) {
        Copy-SherpaOnnxRuntimeLibraries -DestinationDir $outputDir
    }
}
finally {
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}

Write-Output "Built $outputPath ($MediaBackend media backend)"
