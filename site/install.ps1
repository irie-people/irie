# Irie's installer for Windows, in PowerShell:
#
#   powershell -ExecutionPolicy ByPass -c "irm https://irie-people.github.io/irie/install.ps1 | iex"
#
# Irie runs inside WSL 2, Windows' own Linux, as it runs on any Linux. This
# downloads irie.exe, the small program that starts it there, from the
# latest release on GitHub, checks its SHA-256, puts it in
# %LOCALAPPDATA%\irie\bin and adds that folder to your PATH. Then
# `irie setup` installs what is missing (WSL, Ubuntu, Irie inside it, and
# its shortcuts), asking before each step. Running it again updates
# irie.exe; Irie inside WSL updates the same way it does on Linux.
#
# What it reads from the environment:
#
#   $env:IRIE_VERSION = "1.3.0"   install that release, not the latest
#   $env:IRIE_NO_SETUP = "1"      don't run irie setup at the end
#   $env:IRIE_DOWNLOADS = "URL"   where the release files are, such as a mirror
#
# The source of this file is site/install.ps1 in github.com/irie-people/irie.

# All of it runs in Install-Irie, so a download cut short runs nothing.
function Install-Irie {
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue'
	$repo = 'https://github.com/irie-people/irie'
	$docs = "$repo/blob/main/docs/WINDOWS.md"

	# A 32-bit or emulated shell reports its own processor; the machine's
	# is in PROCESSOR_ARCHITEW6432 then.
	$cpu = $env:PROCESSOR_ARCHITECTURE
	if ($env:PROCESSOR_ARCHITEW6432) { $cpu = $env:PROCESSOR_ARCHITEW6432 }
	switch ($cpu) {
		'AMD64' { $arch = 'amd64' }
		'ARM64' { $arch = 'arm64' }
		default { throw "There is no Irie for a $cpu processor. See $docs" }
	}

	if ($env:IRIE_DOWNLOADS) {
		$base = $env:IRIE_DOWNLOADS.TrimEnd('/')
	} elseif ($env:IRIE_VERSION) {
		$base = "$repo/releases/download/v$($env:IRIE_VERSION.TrimStart('v'))"
	} else {
		$base = "$repo/releases/latest/download"
	}
	$name = "irie-windows-$arch.zip"

	# Windows PowerShell 5.1 may not offer TLS 1.2 by itself, which GitHub
	# needs.
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

	$tmp = Join-Path ([IO.Path]::GetTempPath()) ('irie-install-' + [guid]::NewGuid().ToString('N'))
	New-Item -ItemType Directory -Path $tmp | Out-Null
	try {
		Write-Host "Downloading $name"
		try {
			Invoke-WebRequest -UseBasicParsing -Uri "$base/$name" -OutFile (Join-Path $tmp $name)
			Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')
		} catch {
			throw "Could not download $base/$name ($($_.Exception.Message)). A release before Windows had a download has none. See $docs"
		}
		$want = $null
		foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
			$f = $line.Trim() -split '\s+', 2
			if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $name) { $want = $f[0].ToLower() }
		}
		if (-not $want) { throw "SHA256SUMS has no line for $name" }
		$got = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $name)).Hash.ToLower()
		if ($got -ne $want) { throw "The SHA-256 of $name is $got, not the release's $want; nothing was installed" }

		Expand-Archive -Path (Join-Path $tmp $name) -DestinationPath (Join-Path $tmp 'x') -Force
		$bin = Join-Path $env:LOCALAPPDATA 'irie\bin'
		New-Item -ItemType Directory -Force -Path $bin | Out-Null
		$exe = Join-Path $bin 'irie.exe'
		# A running irie.exe can't be written over, but it can be moved
		# aside; the copy left from the last update goes first, unless it
		# still runs.
		$old = "$exe.old"
		if (Test-Path $old) { Remove-Item -Force $old -ErrorAction SilentlyContinue }
		if (Test-Path $exe) {
			if (Test-Path $old) { $old = "$exe.$([guid]::NewGuid().ToString('N')).old" }
			Move-Item -Force $exe $old
		}
		Copy-Item -Force (Join-Path $tmp 'x\irie.exe') $exe
		Copy-Item -Force (Join-Path $tmp 'x\LICENSE') (Join-Path $bin 'LICENSE')
		Write-Host "Installed $exe"

		$path = [Environment]::GetEnvironmentVariable('Path', 'User')
		$parts = @()
		if ($path) { $parts = $path -split ';' | Where-Object { $_ } }
		if ($parts -notcontains $bin) {
			[Environment]::SetEnvironmentVariable('Path', (($parts + $bin) -join ';'), 'User')
			Write-Host "Added $bin to your PATH; a new terminal finds irie there"
		}
		if (($env:Path -split ';') -notcontains $bin) { $env:Path = "$env:Path;$bin" }
	} finally {
		Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
	}

	if ($env:IRIE_NO_SETUP) { return }
	& $exe setup
}

Install-Irie
