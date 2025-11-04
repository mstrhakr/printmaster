# Extract all comments from Go source files
# Output format: filename:line:col: comment text

param(
    [string]$Path = ".",
    [string]$OutputFile = "all_comments.txt"
)

Write-Host "Scanning for Go files in: $Path" -ForegroundColor Cyan

# Find all .go files recursively
$goFiles = Get-ChildItem -Path $Path -Filter "*.go" -Recurse -File

Write-Host "Found $($goFiles.Count) Go files" -ForegroundColor Green

$allComments = @()

foreach ($file in $goFiles) {
    Write-Host "Processing: $($file.FullName)" -ForegroundColor Gray
    
    # Read file and find all comment lines
    $lineNumber = 0
    Get-Content $file.FullName | ForEach-Object {
        $lineNumber++
        $line = $_
        
        # Find single-line comments (// ...)
        if ($line -match '^\s*//') {
            # Calculate column position (1-based, where comment starts)
            $leadingWhitespace = $line -replace '^(\s*)//.+$', '$1'
            $col = $leadingWhitespace.Length + 1
            
            # Get relative path from root
            $relativePath = $file.FullName.Replace((Get-Location).Path + "\", "").Replace("\", "/")
            
            $allComments += "${relativePath}:${lineNumber}:${col}: $($line.TrimStart())"
        }
    }
}

# Save to output file
$allComments | Out-File -FilePath $OutputFile -Encoding UTF8

Write-Host "`nTotal comments found: $($allComments.Count)" -ForegroundColor Green
Write-Host "Saved to: $OutputFile" -ForegroundColor Cyan

# Show summary by file
Write-Host "`n=== Comment Summary by File ===" -ForegroundColor Yellow
$allComments | ForEach-Object {
    if ($_ -match '^([^:]+):') {
        $matches[1]
    }
} | Group-Object | Sort-Object Count -Descending | Select-Object -First 10 | Format-Table Name, Count -AutoSize

# Show legacy/removal comments
Write-Host "`n=== Legacy/Removal Comments ===" -ForegroundColor Yellow
$legacyComments = $allComments | Where-Object { 
    $_ -match '(REMOVED|removed|Legacy|legacy|deprecated|Deprecated|no longer|obsolete|Obsolete|TODO|FIXME|HACK|XXX)'
}

if ($legacyComments.Count -gt 0) {
    Write-Host "Found $($legacyComments.Count) legacy/removal comments" -ForegroundColor Cyan
    $legacyComments | Out-File -FilePath "comments_legacy.txt" -Encoding UTF8
    Write-Host "Saved to: comments_legacy.txt" -ForegroundColor Cyan
    Write-Host "`nFirst 20 legacy comments:" -ForegroundColor Gray
    $legacyComments | Select-Object -First 20 | ForEach-Object { Write-Host "  $_" }
} else {
    Write-Host "No legacy/removal comments found" -ForegroundColor Green
}
