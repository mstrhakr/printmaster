/**
 * PrintMaster Diagnostic Report Proxy
 * 
 * Receives device diagnostic reports from PrintMaster agents,
 * creates a GitHub Gist with the data, and returns URLs for
 * creating a pre-filled GitHub issue.
 */

export default {
  async fetch(request, env) {
    // Handle CORS preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, {
        headers: {
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Methods': 'POST, OPTIONS',
          'Access-Control-Allow-Headers': 'Content-Type',
          'Access-Control-Max-Age': '86400',
        },
      });
    }

    // Only allow POST to /diagnostic
    const url = new URL(request.url);
    if (url.pathname !== '/diagnostic' || request.method !== 'POST') {
      return jsonResponse({ success: false, error: 'Not found' }, 404);
    }

    try {
      const body = await request.json();
      const report = body.report;

      if (!report || !report.report_id) {
        return jsonResponse({ success: false, error: 'Invalid report format' }, 400);
      }

      // Create Gist with diagnostic data
      const gistResponse = await fetch('https://api.github.com/gists', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${env.GITHUB_PAT}`,
          'Accept': 'application/vnd.github+json',
          'User-Agent': 'PrintMaster-Diagnostic-Proxy/1.0',
          'X-GitHub-Api-Version': '2022-11-28',
        },
        body: JSON.stringify({
          description: `PrintMaster Device Report: ${report.issue_type} - ${report.device_model || 'Unknown'}`,
          public: false,
          files: {
            'diagnostic_report.json': {
              content: JSON.stringify(report, null, 2),
            },
            'summary.md': {
              content: generateSummary(report),
            },
          },
        }),
      });

      if (!gistResponse.ok) {
        const errorText = await gistResponse.text();
        console.error('GitHub API error:', errorText);
        return jsonResponse({ 
          success: false, 
          error: `Failed to create gist: ${gistResponse.status}` 
        }, 502);
      }

      const gist = await gistResponse.json();
      const gistUrl = gist.html_url;

      // Build pre-filled issue URL
      const issueUrl = buildIssueUrl(env.GITHUB_REPO, report, gistUrl);

      return jsonResponse({
        success: true,
        gist_url: gistUrl,
        issue_url: issueUrl,
      }, 200);

    } catch (error) {
      console.error('Worker error:', error);
      return jsonResponse({ 
        success: false, 
        error: error.message 
      }, 500);
    }
  },
};

function jsonResponse(data, status) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
    },
  });
}

function generateSummary(report) {
  const issueTypeLabels = {
    'wrong_manufacturer': 'Wrong Manufacturer Detection',
    'wrong_model': 'Wrong Model Detection',
    'missing_serial': 'Missing Serial Number',
    'wrong_serial': 'Wrong Serial Number',
    'incorrect_counters': 'Incorrect Page Counters',
    'missing_toner': 'Missing Toner/Ink Levels',
    'missing_supplies': 'Missing Supplies Data',
    'wrong_hostname': 'Wrong Hostname',
    'other': 'Other Issue',
  };

  return `# Device Data Report

## Issue Type
${issueTypeLabels[report.issue_type] || report.issue_type}

## Expected Value
${report.expected_value || 'Not specified'}

## User Message
${report.user_message || 'No additional message'}

## Current Detection Results
- **Manufacturer**: ${report.current_manufacturer || 'Unknown'}
- **Model**: ${report.current_model || 'Unknown'}
- **Serial**: ${report.current_serial || 'Unknown'}
- **Page Count**: ${report.current_page_count || 'N/A'}

## Device Info
- **IP**: ${report.device_ip || 'Unknown'}
- **MAC**: ${report.device_mac || 'Unknown'}
- **Detected Vendor**: ${report.detected_vendor || 'Unknown'}

## Agent Info
- **Version**: ${report.agent_version || 'Unknown'}
- **OS**: ${report.os || 'Unknown'}
- **Arch**: ${report.arch || 'Unknown'}

## Detection Steps
${(report.detection_steps || []).map(s => '- ' + s).join('\n') || 'No steps recorded'}

## SNMP Responses
\`\`\`json
${JSON.stringify(report.snmp_responses || [], null, 2)}
\`\`\`

---
*Report ID: ${report.report_id}*  
*Timestamp: ${report.timestamp}*
`;
}

function buildIssueUrl(repo, report, gistUrl) {
  const issueTypeLabels = {
    'wrong_manufacturer': 'Wrong manufacturer detection',
    'wrong_model': 'Wrong model detection',
    'missing_serial': 'Missing serial number',
    'wrong_serial': 'Wrong serial number',
    'incorrect_counters': 'Incorrect page counters',
    'missing_toner': 'Missing toner/ink levels',
    'missing_supplies': 'Missing supplies data',
    'wrong_hostname': 'Wrong hostname',
    'other': 'Other',
  };

  const title = `[Device Report] ${report.issue_type} – ${report.device_model || 'Unknown Device'}`;
  
  const params = new URLSearchParams({
    template: 'device-report.yml',
    title: title,
    gist_url: gistUrl,
    issue_type: issueTypeLabels[report.issue_type] || 'Other',
    expected_value: report.expected_value || '',
    device_model: report.device_model || '',
    device_manufacturer: report.current_manufacturer || '',
  });

  return `https://github.com/${repo}/issues/new?${params.toString()}`;
}
