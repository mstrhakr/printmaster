#!/usr/bin/env pwsh
# Interactive comment cleanup script
# Prompts user for each comment to decide: keep (n) or remove (y)

param(
    [string]$CommentFile = "comments_legacy.txt"
)

if (-not (Test-Path $CommentFile)) {
    Write-Host "Error: $CommentFile not found" -ForegroundColor Red
    exit 1
}

# Read all comment lines
$comments = Get-Content $CommentFile

Write-Host "Interactive Comment Cleanup" -ForegroundColor Cyan
Write-Host "=============================" -ForegroundColor Cyan
Write-Host "Found $($comments.Count) comments to review" -ForegroundColor Yellow
Write-Host ""

$toRemove = @()

foreach ($line in $comments) {
    if ($line -match '^(.+?):(\d+):(\d+):\s*(.+)$') {
        $file = $matches[1]
        $lineNum = [int]$matches[2]
        $col = [int]$matches[3]
        $comment = $matches[4]
        
        # Display context
        Write-Host "`n$file : $lineNum" -ForegroundColor Cyan
        Write-Host $comment -ForegroundColor Gray
        
        # Prompt for decision
        $response = Read-Host "Remove this comment? (y/n)"
        
        if ($response -eq 'y' -or $response -eq 'Y') {
            $toRemove += [PSCustomObject]@{
                File = $file
                Line = $lineNum
                Col = $col
                Comment = $comment
            }
            Write-Host "  -> Marked for removal" -ForegroundColor Green
        } else {
            Write-Host "  -> Keeping" -ForegroundColor Yellow
        }
    }
}

Write-Host "`n=============================" -ForegroundColor Cyan
Write-Host "Review complete: $($toRemove.Count) comments marked for removal" -ForegroundColor Green

if ($toRemove.Count -eq 0) {
    Write-Host "No changes to make." -ForegroundColor Yellow
    exit 0
}

# Group by file and sort by line number descending (so we can remove from bottom up)
$byFile = $toRemove | Group-Object File

Write-Host "`nRemoving comments..." -ForegroundColor Cyan

foreach ($group in $byFile) {
    $filePath = $group.Name
    Write-Host "Processing $filePath ..." -ForegroundColor Yellow
    
    if (-not (Test-Path $filePath)) {
        Write-Host "  Warning: File not found, skipping" -ForegroundColor Red
        continue
    }
    
    # Read file content
    $content = Get-Content $filePath
    
    # Sort by line descending so we can remove from bottom up without affecting line numbers
    $removes = $group.Group | Sort-Object Line -Descending
    
    foreach ($item in $removes) {
        $lineIdx = $item.Line - 1  # Convert to 0-based index
        
        if ($lineIdx -ge 0 -and $lineIdx -lt $content.Count) {
            # Verify this is actually a comment line
            if ($content[$lineIdx] -match '^\s*//') {
                Write-Host "  Removing line $($item.Line): $($item.Comment.Substring(0, [Math]::Min(60, $item.Comment.Length)))..." -ForegroundColor Gray
                $content = $content[0..($lineIdx-1)] + $content[($lineIdx+1)..($content.Count-1)]
            } else {
                Write-Host "  Warning: Line $($item.Line) is not a comment, skipping" -ForegroundColor Red
            }
        }
    }
    
    # Write back to file
    $content | Set-Content $filePath -Encoding UTF8
    Write-Host "  Saved $filePath" -ForegroundColor Green
}

Write-Host "`nCleanup complete!" -ForegroundColor Green
Write-Host "Removed $($toRemove.Count) comments across $($byFile.Count) files" -ForegroundColor Cyan
