const vscode = require('vscode');
const fs = require('fs');
const path = require('path');
const cp = require('child_process');

let currentWatcher = null;
let currentConvId = null;
let currentConvTitle = 'Active Session';
let debounceTimer = null;
let cachedHandoffTarget = 150000;
let lastMetrics = {
    total: 0,
    user: 0,
    model: 0,
    thinking: 0,
    steps: 0
};

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

function getAntigravityDir() {
    return process.env.ANTIGRAVITY_DIR ||
           process.env.ANTIGRAVITY_HOME ||
           path.join(process.env.HOME || '', '.gemini', 'antigravity');
}

function getCurrentWorkspaceUri() {
    const folders = vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) {
        return folders[0].uri.toString();
    }
    return '';
}

function getHandoffConfig() {
    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    const settingVal = config.get('handoffTarget');
    if (typeof settingVal === 'number') {
        return Promise.resolve(settingVal);
    }
    return Promise.resolve(150000);
}

function setHandoffConfig(val) {
    const config = vscode.workspace.getConfiguration('antigravity.tokenMonitor');
    return config.update('handoffTarget', val, vscode.ConfigurationTarget.Global);
}



function triggerNextTurnHandoff() {
    const agyDir = getAntigravityDir();
    const stateFile = path.join(agyDir, 'token_hook_state.json');
    try {
        let state = {};
        if (fs.existsSync(stateFile)) {
            try {
                state = JSON.parse(fs.readFileSync(stateFile, 'utf8'));
            } catch (e) {}
        }
        if (currentConvId) {
            state[currentConvId] = state[currentConvId] || {};
            state[currentConvId].forceImmediate = true;
            state[currentConvId].notified = false;
            fs.writeFileSync(stateFile, JSON.stringify(state, null, 2), 'utf8');
            vscode.window.showInformationMessage('Hand-off directive queued for next step via hook. The agent will prepare the handoff summary on its next response.');
        } else {
            vscode.window.showWarningMessage('No active conversation detected to queue handoff.');
        }
    } catch (e) {
        vscode.window.showErrorMessage('Failed to queue handoff directive: ' + e.message);
    }
}

// In-memory transcript parsing: takes ~1ms, zero subprocesses, zero SQLite locking
function parseTranscriptMetrics(filePath) {
    try {
        if (!fs.existsSync(filePath)) return null;
        const content = fs.readFileSync(filePath, 'utf8');
        const lines = content.trim().split('\n');
        let totalChars = 0;
        let userChars = 0;
        let modelChars = 0;
        let thinkingChars = 0;
        let steps = 0;

        for (const line of lines) {
            if (!line.trim()) continue;
            try {
                const s = JSON.parse(line);
                steps++;
                const c = (s.content ? s.content.length : 0);
                const th = (s.thinking ? s.thinking.length : 0);
                const tc = (s.tool_calls ? JSON.stringify(s.tool_calls).length : 0);
                
                totalChars += (c + th + tc);
                thinkingChars += th;
                if (s.source === 'USER_EXPLICIT') {
                    userChars += c;
                } else if (s.source === 'MODEL') {
                    modelChars += (c + th + tc);
                }
            } catch (e) {}
        }

        const totalTokens = Math.round(totalChars / 3.8);
        const userTokens = Math.round(userChars / 3.8);
        const modelTokens = Math.round(modelChars / 3.8);
        const thinkingTokens = Math.round(thinkingChars / 3.8);

        return {
            total: totalTokens,
            user: userTokens,
            model: modelTokens,
            thinking: thinkingTokens,
            steps: steps
        };
    } catch (e) {
        return null;
    }
}

function resolveActiveConversation(callback) {
    const agyDir = getAntigravityDir();
    const sumDb = path.join(agyDir, 'conversation_summaries.db');
    const wsUri = getCurrentWorkspaceUri();

    if (fs.existsSync(sumDb) && wsUri) {
        const query = "SELECT conversation_id, title FROM conversation_summaries WHERE workspace_uris LIKE '%" + wsUri + "%' ORDER BY last_modified_time DESC LIMIT 1;";
        cp.exec('sqlite3 ' + sumDb + ' "' + query + '"', (err, stdout) => {
            if (!err && stdout && stdout.trim()) {
                const parts = stdout.trim().split('|');
                const convId = parts[0];
                const title = parts[1] || 'Active Session';
                callback(convId, title);
            } else {
                fallbackFindRecent(callback);
            }
        });
    } else {
        fallbackFindRecent(callback);
    }
}

function fallbackFindRecent(callback) {
    const agyDir = getAntigravityDir();
    const brainDir = path.join(agyDir, 'brain');
    if (!fs.existsSync(brainDir)) return callback(null, null);

    try {
        const dirs = fs.readdirSync(brainDir, { withFileTypes: true }).filter(d => d.isDirectory());
        let latestConv = null;
        let latestMtime = 0;
        for (const d of dirs) {
            const tFile = path.join(brainDir, d.name, '.system_generated', 'logs', 'transcript.jsonl');
            try {
                const stat = fs.statSync(tFile);
                if (stat.mtimeMs > latestMtime) {
                    latestMtime = stat.mtimeMs;
                    latestConv = d.name;
                }
            } catch(e) {}
        }
        callback(latestConv, 'Active Session');
    } catch(e) {
        callback(null, null);
    }
}

function activate(context) {
    const initialHandoffLabel = cachedHandoffTarget > 0 ? (Math.round(cachedHandoffTarget / 1000) + 'k') : 'off';
    const tokenStatusBar = vscode.window.createStatusBarItem('agy-tokens', vscode.StatusBarAlignment.Right, 101);
    tokenStatusBar.name = 'AGY Tokens & Handoff';
    tokenStatusBar.text = 'AGY: 0k handoff ' + initialHandoffLabel;
    tokenStatusBar.tooltip = 'Click to configure handoff or view token report';
    tokenStatusBar.command = 'antigravity.configureHandoff';
    tokenStatusBar.show();
    context.subscriptions.push(tokenStatusBar);

    function updateDisplay(metrics, title) {
        if (!metrics) return;
        lastMetrics = metrics;
        const total = metrics.total;
        const user = metrics.user;
        const model = metrics.model;
        const thinking = metrics.thinking;

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

        const wsUri = getCurrentWorkspaceUri();
        const wsName = wsUri ? path.basename(wsUri) : 'General';

        tokenStatusBar.tooltip = 'Session: ' + title + '\n' +
                                 '• Workspace: ' + wsName + '\n' +
                                 '• Total Tokens: ' + total.toLocaleString() + ' (' + fmtTotal + ')\n' +
                                 '• Handoff Target: ' + (cachedHandoffTarget > 0 ? cachedHandoffTarget.toLocaleString() + ' tok' : 'Off') + '\n' +
                                 '• Next Internal Compression: ' + fmtNextComp + ' (in ~' + fmtTokensLeft + ' | Cycle C' + cycle + ')\n' +
                                 '• User: ' + fmtUser + ' tok | Model: ' + fmtModel + ' tok | Thinking: ' + fmtThinking + ' tok\n\n' +
                                 'Click to configure handoff, inject handoff, or view report';
    }

    function watchActiveConversation(convId, title) {
        if (!convId) return;
        currentConvId = convId;
        currentConvTitle = title || 'Active Session';

        const agyDir = getAntigravityDir();
        const transcriptPath = path.join(agyDir, 'brain', convId, '.system_generated', 'logs', 'transcript.jsonl');

        // Initial sync on startup
        const metrics = parseTranscriptMetrics(transcriptPath);
        if (metrics) {
            updateDisplay(metrics, currentConvTitle);
        }

        // Close previous watcher if active
        if (currentWatcher) {
            try { currentWatcher.close(); } catch(e) {}
            currentWatcher = null;
        }

        // Watch active conversation log. Debounced by 1.2s to detect tool completion mid-flight with zero lag
        if (fs.existsSync(transcriptPath)) {
            try {
                currentWatcher = fs.watch(transcriptPath, (eventType) => {
                    if (debounceTimer) clearTimeout(debounceTimer);
                    debounceTimer = setTimeout(() => {
                        const updated = parseTranscriptMetrics(transcriptPath);
                        if (updated) {
                            updateDisplay(updated, currentConvTitle);
                        }
                    }, 1200);
                });
            } catch(e) {}
        }
    }

    // Refresh active conversation binding
    function refreshActiveSession() {
        resolveActiveConversation((convId, title) => {
            if (convId && convId !== currentConvId) {
                watchActiveConversation(convId, title);
            } else if (convId) {
                const agyDir = getAntigravityDir();
                const transcriptPath = path.join(agyDir, 'brain', convId, '.system_generated', 'logs', 'transcript.jsonl');
                const m = parseTranscriptMetrics(transcriptPath);
                if (m) updateDisplay(m, title || currentConvTitle);
            }
        });
    }

    getHandoffConfig().then(v => {
        cachedHandoffTarget = v;
        refreshActiveSession();
    });

    // Watch conversation_summaries.db so starting a new conversation updates the watcher
    const agyDir = getAntigravityDir();
    const sumDb = path.join(agyDir, 'conversation_summaries.db');
    let dbWatcher = null;
    let dbDebounce = null;
    if (fs.existsSync(sumDb)) {
        try {
            dbWatcher = fs.watch(sumDb, () => {
                if (dbDebounce) clearTimeout(dbDebounce);
                dbDebounce = setTimeout(() => {
                    refreshActiveSession();
                }, 3000);
            });
        } catch(e) {}
    }

    context.subscriptions.push({
        dispose: () => {
            if (currentWatcher) currentWatcher.close();
            if (dbWatcher) dbWatcher.close();
            if (debounceTimer) clearTimeout(debounceTimer);
            if (dbDebounce) clearTimeout(dbDebounce);
        }
    });

    // Update when window focus changes
    context.subscriptions.push(vscode.window.onDidChangeWindowState(state => {
        if (state.focused) {
            refreshActiveSession();
        }
    }));

    // Update on configuration change
    context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(e => {
        if (e.affectsConfiguration('antigravity.tokenMonitor')) {
            getHandoffConfig().then(v => {
                cachedHandoffTarget = v;
                if (lastMetrics) updateDisplay(lastMetrics, currentConvTitle);
            });
        }
    }));

    // Commands
    context.subscriptions.push(vscode.commands.registerCommand('antigravity.configureHandoff', async () => {
        const options = [];

        if (cachedHandoffTarget > 0 && lastMetrics.total >= cachedHandoffTarget) {
            const nextComp = getNextCompression(lastMetrics.total);
            const nextCycleTarget = Math.max(nextComp - 20000, lastMetrics.total + 15000);
            options.push({
                label: '$(debug-step-over) Advance handoff to Next Cycle (' + Math.round(nextCycleTarget / 1000) + 'k)',
                description: 'Current target (' + Math.round(cachedHandoffTarget / 1000) + 'k) passed. Extend handoff to next compression boundary (~' + Math.round(nextComp / 1000) + 'k).',
                target: nextCycleTarget
            });
        }

        options.push(
            { label: '$(sign-out) Inject handoff message next turn', description: 'Force agent to summarize and hand off on its next response without altering target threshold', action: 'now' },
            { label: '$(output) View detailed token report', description: 'Show breakdown of current conversation', action: 'report' },
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
            showReportChannel();
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
                if (lastMetrics) updateDisplay(lastMetrics, currentConvTitle);
            }
        } else {
            cachedHandoffTarget = selected.target;
            await setHandoffConfig(cachedHandoffTarget);
            vscode.window.showInformationMessage('AGY Handoff Target set to ' + (cachedHandoffTarget > 0 ? (cachedHandoffTarget/1000 + 'k') : 'off'));
            if (lastMetrics) updateDisplay(lastMetrics, currentConvTitle);
        }
    }));

    function showReportChannel() {
        const channel = vscode.window.createOutputChannel('AGY Token Metrics');
        channel.clear();
        const t = lastMetrics.total;
        const u = lastMetrics.user;
        const m = lastMetrics.model;
        const th = lastMetrics.thinking;
        const nextComp = getNextCompression(t);
        const cycle = getCompressionCycle(t);

        channel.appendLine('==================================================');
        channel.appendLine('            AGY TOKEN METRICS REPORT             ');
        channel.appendLine('==================================================');
        channel.appendLine('Session:       ' + currentConvTitle);
        channel.appendLine('ID:            ' + (currentConvId || 'N/A'));
        channel.appendLine('Total Steps:   ' + lastMetrics.steps);
        channel.appendLine('Total Tokens:  ' + t.toLocaleString() + ' (~' + Math.round(t/1000) + 'k)');
        channel.appendLine('  • User:      ' + u.toLocaleString() + ' tok (' + (t>0?Math.round((u/t)*100):0) + '%)');
        channel.appendLine('  • Model:     ' + m.toLocaleString() + ' tok (' + (t>0?Math.round((m/t)*100):0) + '%)');
        channel.appendLine('  • Thinking:  ' + th.toLocaleString() + ' tok (' + (t>0?Math.round((th/t)*100):0) + '%)');
        channel.appendLine('--------------------------------------------------');
        channel.appendLine('Compression Cycle:      Cycle C' + cycle);
        channel.appendLine('Next Compression Cliff: ~' + Math.round(nextComp/1000) + 'k tokens');
        channel.appendLine('Headroom Remaining:     ~' + Math.max(0, nextComp - t).toLocaleString() + ' tokens');
        channel.appendLine('Handoff Target:         ' + (cachedHandoffTarget > 0 ? cachedHandoffTarget.toLocaleString() + ' tokens' : 'Off'));
        channel.appendLine('==================================================');
        channel.show(true);
    }

    context.subscriptions.push(vscode.commands.registerCommand('antigravity.showTokenSummary', () => {
        showReportChannel();
    }));

    context.subscriptions.push(vscode.commands.registerCommand('antigravity.injectHandoffNextTurn', () => {
        triggerNextTurnHandoff();
    }));
}

function deactivate() {
    if (currentWatcher) {
        try { currentWatcher.close(); } catch(e) {}
    }
}

module.exports = {
    activate,
    deactivate
};
