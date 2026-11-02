# Instala a CLI do BR Panel no SEU computador (Windows) para controlar um painel remoto.
#
#   irm https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.ps1 | iex
#
# Uma versão específica:  $env:BRPANEL_VERSION = 'v0.2.0'; irm ... | iex
# Instala em %LOCALAPPDATA%\Programs\brpanel\brpanel.exe e põe a pasta no PATH do usuário (sem pedir
# administrador). Depois, abra um PowerShell novo e rode:  brpanel login ENDEREÇO-DO-PAINEL
# (o token se cria na aba CLI do painel). BRPANEL_BASE_URL aponta para um espelho dos arquivos da release
# e BRPANEL_NO_PATH=1 não mexe no PATH.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = 'viniciusdev772/br_panel'
$version = if ($env:BRPANEL_VERSION) { $env:BRPANEL_VERSION } else { 'latest' }
$base = $env:BRPANEL_BASE_URL
if (-not $base) {
    $base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
}
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "brpanel-cli-windows-$arch.exe"
$dir = Join-Path $env:LOCALAPPDATA 'Programs\brpanel'
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("brpanel-" + [guid]::NewGuid().ToString() + '.exe')

try {
    Write-Host "==> baixando $name ($version)"
    Invoke-WebRequest "$base/$name" -OutFile $tmp -UseBasicParsing
    $r = Invoke-WebRequest "$base/$name.sha256" -UseBasicParsing
    $txt = if ($r.Content -is [byte[]]) { [Text.Encoding]::ASCII.GetString($r.Content) } else { [string]$r.Content }
    $want = ($txt.Trim() -split '\s+')[0]
    $got = (Get-FileHash $tmp -Algorithm SHA256).Hash
    if (-not $want -or $want -ine $got) {
        throw 'a soma SHA-256 não confere: download corrompido ou adulterado; nada foi instalado'
    }
    New-Item -ItemType Directory -Force $dir | Out-Null
    Move-Item -Force $tmp (Join-Path $dir 'brpanel.exe')
} finally {
    if (Test-Path $tmp) { Remove-Item -Force $tmp }
}
Write-Host "==> instalado em $dir\brpanel.exe"

if (-not $env:BRPANEL_NO_PATH) {
    $user = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($user -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', ($(if ($user) { "$user;" } else { '' }) + $dir), 'User')
        Write-Host "==> $dir adicionado ao PATH do usuário (vale em PowerShells novos)"
    }
    if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }
}
Write-Host ''
Write-Host 'Pronto. Próximos passos:'
Write-Host '  1. No painel, aba CLI > Acesso remoto: crie um token.'
Write-Host '  2. brpanel login ENDEREÇO-DO-PAINEL'
Write-Host '  3. brpanel status'
Write-Host 'Dúvidas: brpanel help   |   guia para agentes de IA: brpanel guide'
