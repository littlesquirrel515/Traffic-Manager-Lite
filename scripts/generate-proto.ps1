$ErrorActionPreference='Stop'
$repositoryRoot=Split-Path $PSScriptRoot -Parent
$moduleRoot=Join-Path $repositoryRoot '.tools/gopath/pkg/mod'
$outputRoot=Join-Path $repositoryRoot 'internal/proto'
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
foreach($entry in @(@('xray','github.com/xtls/xray-core@v1.260327.0'),@('v2fly','github.com/v2fly/v2ray-core/v5@v5.54.2'))){
    $kind=$entry[0]
    $source=Join-Path $moduleRoot ($entry[1]+'/app/stats/command/command.proto')
    $schema=Get-Content -LiteralPath $source -Raw
    $schema=$schema -replace 'option go_package = "[^"]+";',('option go_package = "traffic-manager-lite/internal/proto/'+$kind+';'+$kind+'pb";')
    # Core-specific registration extensions are irrelevant to the StatsService wire protocol.
    $schema=$schema -replace 'import "common/protoext/extensions.proto";',''
    if($kind -eq 'v2fly'){$schema=$schema -replace '(?s)message Config \{.*?\}','message Config {}'}
    $directory=Join-Path $outputRoot $kind
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $directory ($kind+'.proto')),$schema,[Text.UTF8Encoding]::new($false))
}
Push-Location $repositoryRoot
try {
    & '.tools/protoc/bin/protoc.exe' '--proto_path=.' '--proto_path=.tools/protoc/include' '--plugin=protoc-gen-go=.tools/bin/protoc-gen-go.exe' '--plugin=protoc-gen-go-grpc=.tools/bin/protoc-gen-go-grpc.exe' '--go_out=.' '--go_opt=paths=source_relative' '--go-grpc_out=.' '--go-grpc_opt=paths=source_relative' 'internal/proto/xrayhandler/handler.proto' 'internal/proto/xray/xray.proto' 'internal/proto/v2fly/v2fly.proto' 'internal/proto/singbox/singbox.proto' 'internal/proto/singboxnative/native.proto'
    if($LASTEXITCODE -ne 0){throw 'protoc failed'}
} finally {Pop-Location}
