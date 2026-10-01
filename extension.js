const vscode = require('vscode');
const fs = require('fs');
const path = require('path');
const cp = require('child_process');

let lastKnownTokens = 0;
let cachedHandoffTarget = 150000;
const COMPRESSION_MILESTONES = [170000, 340000, 510000, 680000, 850000];

function getNextCompression(total) {
    for (const m of COMPRESSION_MILESTONES) {
        if (m > total) return m;
    }
    return 1000000;
}

function getCompressionCycle(total) {
    let cycle = 0;
    for (const m of COMPRESSION_MILESTONES) {
        if (total >= m) cycle++;
    }
    return cycle;
}

function getColorForTokens(total, handoffTarget) {
    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    const enableColors = config.get('enableWarningColors', true);
    if (!enableColors) return undefined;

    const headroom = config.get('compressionHeadroomBuffer', 25000);
    const nextComp = getNextCompression(total);
    const distanceToComp = nextComp - total;
    
    if ((handoffTarget > 0 && total >= handoffTarget) || distanceToComp <= 10000) {
        return new vscode.ThemeColor('errorForeground');
    } else if (distanceToComp <= headroom || (handoffTarget > 0 && (handoffTarget - total) <= 15000)) {
        return new vscode.ThemeColor('editorWarning.foreground');
    }
    return undefined;
}

function getHandoffConfig() {
    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    const settingVal = config.get('handoffTarget');
    if (typeof settingVal === 'number') {
        return Promise.resolve(settingVal);
    }
    const dbPath = path.join(process.env.HOME || '', '.gemini/antigravity/token_metrics.db');
    return new Promise((resolve) => {
        if (!fs.existsSync(dbPath)) return resolve(150000);
        cp.exec('sqlite3 ' + dbPath + ' "CREATE TABLE IF NOT EXISTS system_config (key TEXT PRIMARY KEY, value TEXT); SELECT value FROM system_config WHERE key=\'handoff_target\';"', (err, stdout) => {
            if (!err && stdout && stdout.trim()) {
                resolve(parseInt(stdout.trim(), 10) || 150000);
            } else {
                resolve(150000);
            }
        });
    });
}

function setHandoffConfig(val) {
    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    config.update('handoffTarget', val, vscode.ConfigurationTarget.Global);

    const dbPath = path.join(process.env.HOME || '', '.gemini/antigravity/token_metrics.db');
    return new Promise((resolve) => {
        if (!fs.existsSync(dbPath)) return resolve();
        cp.exec('sqlite3 ' + dbPath + ' "INSERT OR REPLACE INTO system_config (key, value) VALUES (\'handoff_target\', \'' + val + '\');"', () => {
            resolve();
        });
    });
}

function triggerNextTurnHandoff() {
    const rulesDir = path.join(process.env.HOME || '', '.gemini/antigravity/rules');
    const ruleFile = path.join(rulesDir, 'context_handoff.md');
    if (!fs.existsSync(rulesDir)) fs.mkdirSync(rulesDir, { recursive: true });
    
    const content = "---\n" +
        "description: User triggered manual handoff directive\n" +
        "---\n\n" +
        "# ⚠️ IMMEDIATE CONTEXT HANDOFF REQUESTED BY USER\n" +
        "The user has explicitly triggered an early handoff before starting a new major task.\n\n" +
        "## INSTRUCTIONS FOR ASSISTANT:\n" +
        "1. Conclude this turn immediately with a structured **HANDOFF SUMMARY**:\n" +
        "   - **Status & Accomplishments**: What was just completed in this session.\n" +
        "   - **Key Context / Files**: Specific files modified or created.\n" +
        "   - **Next Action**: The immediate next step to take.\n" +
        "   - **Continuation Prompt**: A copy-pasteable prompt for the user to provide when opening the new chat.\n";
    
    fs.writeFileSync(ruleFile, content, 'utf8');

    const dbPath = path.join(process.env.HOME || '', '.gemini/antigravity/token_metrics.db');
    if (fs.existsSync(dbPath)) {
        cp.exec('sqlite3 ' + dbPath + ' "INSERT OR REPLACE INTO system_config (key, value) VALUES (\'manual_handoff_flag\', \'1\');"');
    }

    vscode.window.showInformationMessage('Hand-off directive injected for next turn. The agent will prepare the handoff summary on its next response.');
}

function getCurrentWorkspaceUri() {
    const folders = vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) {
        return folders[0].uri.toString();
    }
    return '';
}

function activate(context) {
    const initialHandoffLabel = cachedHandoffTarget > 0 ? (Math.round(cachedHandoffTarget / 1000) + 'k') : 'off';
    const tokenStatusBar = vscode.window.createStatusBarItem('agy-tokens', vscode.StatusBarAlignment.Right, 101);
    tokenStatusBar.name = 'AGY Tokens & Handoff';
    tokenStatusBar.text = 'AGY: 0k handoff ' + initialHandoffLabel;
    tokenStatusBar.tooltip = 'Click to configure handoff, inject handoff, or view token report';
    tokenStatusBar.command = 'antigravity.configureHandoff';
    tokenStatusBar.show();
    context.subscriptions.push(tokenStatusBar);

    getHandoffConfig().then(v => {
        cachedHandoffTarget = v;
        updateTokenStatusBar();
    });

    function updateTokenStatusBar() {
        try {
            const metricsDbPath = path.join(process.env.HOME || '', '.gemini/antigravity/token_metrics.db');
            const sumDbPath = path.join(process.env.HOME || '', '.gemini/antigravity/conversation_summaries.db');
            const wsUri = getCurrentWorkspaceUri();
            const wsName = wsUri ? path.basename(wsUri) : 'General';
            const handoffLabel = cachedHandoffTarget > 0 ? (Math.round(cachedHandoffTarget / 1000) + 'k') : 'off';

            if (!wsUri || !fs.existsSync(metricsDbPath) || !fs.existsSync(sumDbPath)) {
                tokenStatusBar.color = undefined;
                tokenStatusBar.text = 'AGY: 0k handoff ' + handoffLabel;
                tokenStatusBar.tooltip = 'Workspace: ' + wsName + '\n' +
                                         'No Antigravity conversations recorded in this workspace yet.\n' +
                                         'Tokens will start tracking automatically when a session begins.\n' +
                                         'Click to configure handoff target.';
                return;
            }

            // Build query scoped strictly to THIS window's workspace
            const query = "SELECT m.est_total_tokens, m.est_user_tokens, m.est_model_tokens, m.est_thinking_tokens, s.title " +
                          "FROM conversation_summaries s " +
                          "JOIN mdb.conversation_summary m ON s.conversation_id = m.conversation_id " +
                          "WHERE s.workspace_uris LIKE '%" + wsUri + "%' " +
                          "ORDER BY s.last_modified_time DESC LIMIT 1;";

            const fullCmd = 'sqlite3 ' + sumDbPath + ' "ATTACH \'' + metricsDbPath + '\' AS mdb; ' + query + '"';

            cp.exec(fullCmd, (err, stdout) => {
                
                if (!err && stdout && stdout.trim()) {
                    const parts = stdout.trim().split('|');
                    const total = parseInt(parts[0], 10) || 0;
                    const user = parseInt(parts[1], 10) || 0;
                    const model = parseInt(parts[2], 10) || 0;
                    const thinking = parseInt(parts[3], 10) || 0;
                    const title = parts[4] || '';
                    
                    lastKnownTokens = total;

                    let fmtTotal = total >= 1000000 ? (total/1000000).toFixed(2) + 'M' : (total >= 1000 ? (total/1000).toFixed(1) + 'k' : total);
                    let fmtUser = user >= 1000 ? Math.round(user/1000) + 'k' : user;
                    let fmtModel = model >= 1000 ? Math.round(model/1000) + 'k' : model;
                    let fmtThinking = thinking >= 1000 ? Math.round(thinking/1000) + 'k' : thinking;
                    
                    const nextComp = getNextCompression(total);
                    const cycle = getCompressionCycle(total);
                    const tokensLeftUntilComp = nextComp - total;
                    const fmtTokensLeft = Math.round(tokensLeftUntilComp / 1000) + 'k';
                    const fmtNextComp = Math.round(nextComp / 1000) + 'k';

                    tokenStatusBar.color = getColorForTokens(total, cachedHandoffTarget);

                    let handoffLabel = cachedHandoffTarget > 0 ? (Math.round(cachedHandoffTarget/1000) + 'k') : 'off';
                    tokenStatusBar.text = 'AGY: ' + fmtTotal + ' handoff ' + handoffLabel;

                    tokenStatusBar.tooltip = 'Session: ' + title + '\n' +
                                             '• Workspace: ' + wsName + '\n' +
                                             '• Total Tokens: ' + total.toLocaleString() + ' (' + fmtTotal + ')\n' +
                                             '• Handoff Target: ' + (cachedHandoffTarget > 0 ? cachedHandoffTarget.toLocaleString() + ' tok' : 'Off') + '\n' +
                                             '• Next Internal Compression: ' + fmtNextComp + ' (in ~' + fmtTokensLeft + ' | Cycle C' + cycle + ')\n' +
                                             '• User: ' + fmtUser + ' tok | Model: ' + fmtModel + ' tok | Thinking: ' + fmtThinking + ' tok\n\n' +
                                             'Click to configure handoff, inject handoff, or view report';
                } else {
                    // No conversation exists in this workspace window yet
                    tokenStatusBar.color = undefined;
                    tokenStatusBar.text = 'AGY: 0k handoff ' + (cachedHandoffTarget > 0 ? (Math.round(cachedHandoffTarget/1000) + 'k') : 'off');
                    tokenStatusBar.tooltip = 'Workspace: ' + wsName + '\n' +
                                             'No Antigravity conversations recorded in this workspace yet.\n' +
                                             'Tokens will start tracking automatically when a session begins.\n' +
                                             'Click to configure handoff target.';
                }
            });
        } catch (e) {}
    }

    updateTokenStatusBar();

    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    let pollInterval = config.get('pollIntervalMs', 1000);
    let tokenInterval = setInterval(updateTokenStatusBar, pollInterval);

    context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(e => {
        if (e.affectsConfiguration('antigravity.tokenMonitor')) {
            getHandoffConfig().then(v => {
                cachedHandoffTarget = v;
                updateTokenStatusBar();
            });
            const newInterval = vscode.workspace.getConfiguration('antigravity.tokenMonitor').get('pollIntervalMs', 1000);
            if (newInterval !== pollInterval) {
                clearInterval(tokenInterval);
                pollInterval = newInterval;
                tokenInterval = setInterval(updateTokenStatusBar, pollInterval);
            }
        }
    }));

    context.subscriptions.push({ dispose: () => clearInterval(tokenInterval) });

    context.subscriptions.push(vscode.commands.registerCommand('antigravity.configureHandoff', async () => {
        const options = [];

        // Smarter missed-handoff recovery: if current tokens passed the target, offer one-click advancement to next milestone
        if (cachedHandoffTarget > 0 && lastKnownTokens >= cachedHandoffTarget) {
            const nextComp = getNextCompression(lastKnownTokens);
            const nextCycleTarget = Math.max(nextComp - 20000, lastKnownTokens + 15000);
            options.push({
                label: '$(debug-step-over) Advance handoff to Next Cycle (' + Math.round(nextCycleTarget / 1000) + 'k)',
                description: 'Current target (' + Math.round(cachedHandoffTarget / 1000) + 'k) passed. Extend handoff to next compression boundary (~' + Math.round(nextComp / 1000) + 'k).',
                target: nextCycleTarget
            });
        }

        options.push(
            { label: '$(sign-out) Inject handoff message next turn', description: 'Force agent to summarize and hand off on its next response without altering target threshold', action: 'now' },
            { label: '$(output) View detailed token report', description: 'Show step-by-step breakdown and project metrics', action: 'report' },
            { label: '150k tokens (Pre-Cycle 1)', description: '~20k buffer before 1st compression (~170k)', target: 150000 },
            { label: '320k tokens (Pre-Cycle 2)', description: '~20k buffer before 2nd compression (~340k)', target: 320000 },
            { label: '490k tokens (Pre-Cycle 3)', description: '~20k buffer before 3rd compression (~510k)', target: 490000 },
            { label: '660k tokens (Pre-Cycle 4)', description: '~20k buffer before 4th compression (~680k)', target: 660000 },
            { label: '140k tokens', description: 'Conservative: ~30k buffer before 1st compression', target: 140000 },
            { label: '160k tokens', description: 'Aggressive: ~10k buffer before 1st compression', target: 160000 },
            { label: '$(edit) Custom token amount...', description: 'Type any custom token limit or "off"', custom: true },
            { label: '$(circle-slash) Disable auto-handoff (off)', description: 'Turn off automatic handoff', target: 0 }
        );

        const selected = await vscode.window.showQuickPick(options, {
            placeHolder: 'AGY Token & Handoff Settings (Current Target: ' + (cachedHandoffTarget > 0 ? (cachedHandoffTarget/1000 + 'k') : 'off') + ')'
        });

        if (!selected) return;

        if (selected.action === 'now') {
            triggerNextTurnHandoff();
            return;
        }

        if (selected.action === 'report') {
            cp.exec('/home/jimmy/.local/bin/agy-tokens', (err, stdout) => {
                if (stdout) {
                    const channel = vscode.window.createOutputChannel('AGY Token Metrics');
                    channel.clear();
                    channel.appendLine(stdout);
                    channel.show(true);
                }
            });
            return;
        }

        if (selected.custom) {
            const input = await vscode.window.showInputBox({
                prompt: 'Enter token limit for handoff (e.g. 150000, 150k, off)',
                value: String(cachedHandoffTarget > 0 ? cachedHandoffTarget : 'off'),
                validateInput: (val) => {
                    let cleaned = val.trim().toLowerCase();
                    if (cleaned === 'off' || cleaned === '0') return null;
                    let num = 0;
                    if (cleaned.endsWith('k')) num = parseFloat(cleaned) * 1000;
                    else if (cleaned.endsWith('m')) num = parseFloat(cleaned) * 1000000;
                    else num = parseInt(cleaned, 10);
                    return isNaN(num) || num < 10000 ? 'Please enter a valid number >= 10,000 or "off"' : null;
                }
            });

            if (input) {
                let cleaned = input.trim().toLowerCase();
                let num = 0;
                if (cleaned === 'off' || cleaned === '0') {
                    num = 0;
                } else if (cleaned.endsWith('k')) {
                    num = parseFloat(cleaned) * 1000;
                } else if (cleaned.endsWith('m')) {
                    num = parseFloat(cleaned) * 1000000;
                } else {
                    num = parseInt(cleaned, 10);
                }
                
                cachedHandoffTarget = Math.round(num);
                await setHandoffConfig(cachedHandoffTarget);
                vscode.window.showInformationMessage('AGY Handoff Target set to ' + (cachedHandoffTarget > 0 ? cachedHandoffTarget.toLocaleString() + ' tokens' : 'off'));
                updateTokenStatusBar();
            }
        } else {
            cachedHandoffTarget = selected.target;
            await setHandoffConfig(cachedHandoffTarget);
            vscode.window.showInformationMessage('AGY Handoff Target set to ' + (cachedHandoffTarget > 0 ? (cachedHandoffTarget/1000 + 'k') : 'off'));
            updateTokenStatusBar();
        }
    }));

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

    context.subscriptions.push(vscode.commands.registerCommand('antigravity.injectHandoffNextTurn', () => {
        triggerNextTurnHandoff();
    }));
}

function deactivate() {}

module.exports = {
    activate,
    deactivate
};
