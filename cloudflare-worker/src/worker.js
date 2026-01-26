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
          files: buildGistFiles(report),
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

/**
 * Build separate gist files for easier searching/debugging
 */
function buildGistFiles(report) {
  const files = {};

  // 1. Summary markdown (quick overview)
  files['1_summary.md'] = {
    content: generateSummary(report),
  };

  // 2. Core report (basic info without bulky nested data)
  const coreReport = { ...report };
  delete coreReport.device_record;
  delete coreReport.metrics_history;
  delete coreReport.snmp_responses;
  delete coreReport.raw_data;
  delete coreReport.recent_logs;
  files['2_core_report.json'] = {
    content: JSON.stringify(coreReport, null, 2),
  };

  // 3. Device record (full DB entry)
  if (report.device_record && Object.keys(report.device_record).length > 0) {
    files['3_device_record.json'] = {
      content: JSON.stringify(report.device_record, null, 2),
    };
  }

  // 4. Metrics history (time series data)
  if (report.metrics_history && report.metrics_history.length > 0) {
    files['4_metrics_history.json'] = {
      content: JSON.stringify(report.metrics_history, null, 2),
    };
  }

  // 5. SNMP responses (raw OID data)
  if (report.snmp_responses && report.snmp_responses.length > 0) {
    files['5_snmp_responses.json'] = {
      content: JSON.stringify(report.snmp_responses, null, 2),
    };
  }

  // 6. Raw data (any extra fields from device)
  if (report.raw_data && Object.keys(report.raw_data).length > 0) {
    files['6_raw_data.json'] = {
      content: JSON.stringify(report.raw_data, null, 2),
    };
  }

  // 7. Recent logs (for troubleshooting)
  if (report.recent_logs && report.recent_logs.length > 0) {
    files['7_recent_logs.txt'] = {
      content: report.recent_logs.join('\n'),
    };
  }

  // 8. Full report (everything in one file for complete reference)
  files['8_full_report.json'] = {
    content: JSON.stringify(report, null, 2),
  };

  return files;
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

**Issue Type:** ${issueTypeLabels[report.issue_type] || report.issue_type}

**Device:** ${report.current_manufacturer || 'Unknown'} ${report.current_model || 'Unknown'}

**Expected Value:** ${report.expected_value || 'Not specified'}

**User Notes:** ${report.user_message || 'None'}

---

| Field | Value |
|-------|-------|
| Serial | ${report.current_serial || 'Unknown'} |
| IP | ${report.device_ip || 'N/A'} |
| Agent Version | ${report.agent_version || 'Unknown'} |
| Metrics History | ${report.metrics_history ? report.metrics_history.length + ' snapshots' : 'None'} |

---

## Files in this Gist

| File | Description |
|------|-------------|
| \`2_core_report.json\` | Basic report metadata and current values |
| \`3_device_record.json\` | Full device database entry |
| \`4_metrics_history.json\` | Last 100 metrics snapshots (page counts, toner levels) |
| \`5_snmp_responses.json\` | Raw SNMP OID/value pairs |
| \`6_raw_data.json\` | Additional raw device data |
| \`7_recent_logs.txt\` | Recent agent log entries |
| \`8_full_report.json\` | Complete report (all data) |

*Report ID: ${report.report_id}*
`;
}

function buildIssueUrl(repo, report, gistUrl) {
  // These MUST match the dropdown options in .github/ISSUE_TEMPLATE/device-report.yml exactly
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

  const issueTypeLabel = issueTypeLabels[report.issue_type] || 'Other';
  const title = `[Device Report] ${issueTypeLabel} – ${report.device_model || 'Unknown Device'}`;
  
  const params = new URLSearchParams({
    template: 'device-report.yml',
    title: title,
    gist_url: gistUrl,
    issue_type: issueTypeLabel,
    expected_value: report.expected_value || '',
    device_model: report.device_model || '',
    device_manufacturer: report.current_manufacturer || '',
  });

  return `https://github.com/${repo}/issues/new?${params.toString()}`;
}
