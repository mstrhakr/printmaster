# Extract HTML/CSS/JS from main.go to separate files
param(
    [string]$MainGoPath = ".\main.go",
    [string]$OutputDir = ".\web",
    [switch]$RemoveFromSource,
    [switch]$SkipOutput
)

# Convert to absolute paths
$MainGoPath = Resolve-Path $MainGoPath -ErrorAction Stop
$OutputDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDir)

$content = Get-Content $MainGoPath -Raw

# Find the HTML template boundaries
$htmlStart = $content.IndexOf('fmt.Fprint(w, `<!DOCTYPE html>')
if ($htmlStart -eq -1) {
    Write-Error "Could not find HTML start"
    exit 1
}

if (-not $SkipOutput) {
    Write-Host "Found HTML start at position $htmlStart"
}

# Find the closing backtick and parenthesis
# Look for `)`  (backtick close paren) - need to search from htmlStart + some offset to skip the opening backtick
$searchStart = $htmlStart + 20
$htmlEnd = $content.IndexOf('`)', $searchStart)
if ($htmlEnd -eq -1) {
    Write-Error "Could not find HTML end (could not find backtick-close-paren)"
    exit 1
}

if (-not $SkipOutput) {
    Write-Host "Found HTML end at position $htmlEnd"
}

# Extract the full HTML (skip the "fmt.Fprint(w, `" part)
$htmlStartContent = $htmlStart + 'fmt.Fprint(w, `'.Length
$htmlContent = $content.Substring($htmlStartContent, $htmlEnd - $htmlStartContent)

if (-not $SkipOutput) {
    Write-Host "Extracted HTML content: $($htmlContent.Length) characters"
}

# Extract CSS (between <style> and </style>)
$cssStart = $htmlContent.IndexOf('<style>')
$cssEnd = $htmlContent.IndexOf('</style>')
if ($cssStart -ne -1 -and $cssEnd -ne -1) {
    $cssContent = $htmlContent.Substring($cssStart + 7, $cssEnd - $cssStart - 7).Trim()
    $cssContent | Out-File -FilePath "$OutputDir\style.css" -Encoding UTF8
    if (-not $SkipOutput) {
        Write-Host "Extracted CSS: $($cssContent.Length) characters"
    }
}

# Extract JavaScript (find the last inline <script> tag, not external ones)
# Look for <script> that's NOT followed by src=
$scriptPattern = '<script>'
$lastScriptPos = -1
$searchPos = 0
while ($true) {
    $pos = $htmlContent.IndexOf($scriptPattern, $searchPos)
    if ($pos -eq -1) { break }
    
    # Check if this is an inline script (not <script src=...)
    $nextChars = $htmlContent.Substring($pos + $scriptPattern.Length, [Math]::Min(10, $htmlContent.Length - $pos - $scriptPattern.Length))
    if ($nextChars -notmatch 'src\s*=') {
        $lastScriptPos = $pos
    }
    $searchPos = $pos + 1
}

if ($lastScriptPos -ne -1) {
    $jsEnd = $htmlContent.IndexOf('</script>', $lastScriptPos)
    if ($jsEnd -ne -1) {
        $jsStart = $lastScriptPos + '<script>'.Length
        $jsContent = $htmlContent.Substring($jsStart, $jsEnd - $jsStart).Trim()
        $jsContent | Out-File -FilePath "$OutputDir\app.js" -Encoding UTF8
        if (-not $SkipOutput) {
            Write-Host "Extracted JavaScript: $($jsContent.Length) characters"
        }
    }
}

# Create the HTML template (with CSS and JS references instead of inline)
$htmlTemplate = $htmlContent
$htmlTemplate = $htmlTemplate -replace '<style>[\s\S]*?</style>', '<link rel="stylesheet" href="/static/style.css">'
$htmlTemplate = $htmlTemplate -replace '<script>\s*\n[\s\S]*?</script>', '<script src="/static/app.js"></script>'

$htmlTemplate | Out-File -FilePath "$OutputDir\index.html" -Encoding UTF8
if (-not $SkipOutput) {
    Write-Host "Created HTML template: $($htmlTemplate.Length) characters"
}

# Remove from source if requested
if ($RemoveFromSource) {
    if (-not $SkipOutput) {
        Write-Host "`nRemoving extracted content from source file..."
    }
    
    # Build the new main.go content with embedded file serving
    $beforeHtml = $content.Substring(0, $htmlStart)
    $afterHtml = $content.Substring($htmlEnd + 3)  # Skip `)` and newline
    
    # Create new handler that serves from embedded files
    $newHandler = @'
// Serve the HTML from embedded filesystem
		tmpl, err := template.ParseFS(webFS, "web/index.html")
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, nil)
'@
    
    $newContent = $beforeHtml + $newHandler + "`n" + $afterHtml
    $newContent | Out-File -FilePath $MainGoPath -Encoding UTF8 -NoNewline
    
    if (-not $SkipOutput) {
        Write-Host "Source file updated. Old size: $($content.Length), New size: $($newContent.Length)"
    }
}

if (-not $SkipOutput) {
    Write-Host "`nExtraction complete!"
    Write-Host "Files created in ${OutputDir}:"
    Write-Host "  - index.html"
    Write-Host "  - style.css"
    Write-Host "  - app.js"
    
    if ($RemoveFromSource) {
        Write-Host "`nSource file updated. Don't forget to:"
        Write-Host "  1. Add 'embed' import: import _ `"embed`""
        Write-Host "  2. Add 'html/template' import: import `"html/template`""
        Write-Host "  3. Add embed directive: //go:embed web"
        Write-Host "  4. Add variable: var webFS embed.FS"
        Write-Host "  5. Add static file handler for /static/*"
    }
}
