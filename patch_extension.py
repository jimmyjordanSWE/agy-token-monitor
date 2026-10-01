import re
import os

EXT_PATH = "/home/jimmy/.vscode-server/extensions/google.google-antigravity-1.6.0/extension.js"

with open(EXT_PATH, "r", encoding="utf-8") as f:
    content = f.read()

# Pattern for the previously injected token monitor code
old_block_pattern = r'// --- AGY Token Monitor Status Bar ---.*?context\.subscriptions\.push\(vscode\.commands\.registerCommand\(\'antigravity\.showTokenSummary\'.*?\}\)\);\n'

new_status_bar_code = r"""// --- AGY Token Monitor Status Bar ---
    const fs = require('fs');
    const path = require('path');
    const cp = require('child_process');
    const tokenStatusBar = vscode.window.createStatusBarItem('agy-tokens', vscode.StatusBarAlignment.Right, 101);
    tokenStatusBar.name = 'AGY Tokens';
    tokenStatusBar.text = '$(dashboard) AGY: ...';
    tokenStatusBar.tooltip = 'Click to show token usage report';
    tokenStatusBar.command = 'antigravity.showTokenSummary';
    tokenStatusBar.show();
    context.subscriptions.push(tokenStatusBar);

    let lastKnownTokens = 0;
    let animFrame = 0;
    const spinnerIcons = ['$(sync~spin)', '$(flame)', '$(zap)', '$(dashboard)'];

    function updateTokenStatusBar() {
        try {
            const dbPath = path.join(process.env.HOME || '', '.gemini/antigravity/token_metrics.db');
            if (!fs.existsSync(dbPath)) return;
            
            cp.exec("sqlite3 " + dbPath + " \"SELECT est_total_tokens, est_user_tokens, est_model_tokens, est_thinking_tokens FROM conversation_summary ORDER BY last_updated DESC LIMIT 1;\"", (err, stdout) => {
                if (!err && stdout && stdout.trim()) {
                    const parts = stdout.trim().split('|');
                    const total = parseInt(parts[0], 10) || 0;
                    const user = parseInt(parts[1], 10) || 0;
                    const model = parseInt(parts[2], 10) || 0;
                    const thinking = parseInt(parts[3], 10) || 0;
                    
                    const isIncreasing = total > lastKnownTokens && lastKnownTokens > 0;
                    lastKnownTokens = total;

                    let fmtTotal = total >= 1000000 ? (total/1000000).toFixed(2) + 'M' : (total >= 1000 ? (total/1000).toFixed(1) + 'k' : total);
                    let fmtUser = user >= 1000 ? Math.round(user/1000) + 'k' : user;
                    let fmtModel = model >= 1000 ? Math.round(model/1000) + 'k' : model;
                    let fmtThinking = thinking >= 1000 ? Math.round(thinking/1000) + 'k' : thinking;
                    
                    let icon = '$(dashboard)';
                    if (isIncreasing) {
                        icon = spinnerIcons[animFrame % spinnerIcons.length];
                        animFrame++;
                    }
                    
                    tokenStatusBar.text = icon + ' AGY: ' + fmtTotal + ' tok';
                    tokenStatusBar.tooltip = 'Total: ' + total.toLocaleString() + ' tokens\n• User: ' + fmtUser + ' tok\n• Model: ' + fmtModel + ' tok\n• Thinking: ' + fmtThinking + ' tok\nClick to view full report';
                }
            });
        } catch (e) {}
    }
    updateTokenStatusBar();
    const tokenInterval = setInterval(updateTokenStatusBar, 1000);
    context.subscriptions.push({ dispose: () => clearInterval(tokenInterval) });

    context.subscriptions.push(vscode.commands.registerCommand('antigravity.showTokenSummary', () => {
        cp.exec('/home/jimmy/.local/bin/agy-tokens', (err, stdout) => {
            if (stdout) {
                const channel = vscode.window.createOutputChannel('AGY Token Metrics');
                channel.clear();
                channel.appendLine(stdout);
                channel.show(true);
            }
        });
    }));
"""

if re.search(old_block_pattern, content, flags=re.DOTALL):
    content = re.sub(old_block_pattern, new_status_bar_code, content, flags=re.DOTALL)
    print("Replaced old status bar block.")
else:
    print("Target not found.")

with open(EXT_PATH, "w", encoding="utf-8") as f:
    f.write(content)

print("Patch applied to extension.js successfully.")
