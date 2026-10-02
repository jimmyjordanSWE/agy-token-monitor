const fs = require('fs');
const path = require('path');
const os = require('os');
const cp = require('child_process');

function getAntigravityDir() {
    return process.env.ANTIGRAVITY_DIR ||
           process.env.ANTIGRAVITY_HOME ||
           path.join(os.homedir(), '.gemini', 'antigravity');
}

function cleanAllTitles() {
    const agyDir = getAntigravityDir();
    const annotDir = path.join(agyDir, 'annotations');
    const sumDb = path.join(agyDir, 'conversation_summaries.db');

    // 1. Clean annotations/*.pbtxt
    if (fs.existsSync(annotDir)) {
        try {
            const files = fs.readdirSync(annotDir).filter(f => f.endsWith('.pbtxt'));
            for (const f of files) {
                try {
                    const p = path.join(annotDir, f);
                    const raw = fs.readFileSync(p, 'utf8');
                    const match = raw.match(/title:\s*"([^"]+)"/);
                    if (match && /^\[\d+(\.\d+)?[kM]?\]\s*/.test(match[1])) {
                        const clean = match[1].replace(/^\[\d+(\.\d+)?[kM]?\]\s*/, '');
                        const replaced = raw.replace(/title:\s*"([^"]+)"/, 'title:"' + clean + '"');
                        fs.writeFileSync(p, replaced, 'utf8');
                    }
                } catch (e) {}
            }
        } catch (e) {}
    }

    // 2. Clean conversation_summaries.db
    if (fs.existsSync(sumDb)) {
        try {
            const cleanQuery = "UPDATE conversation_summaries SET title = TRIM(CASE WHEN INSTR(title, ']') > 0 THEN SUBSTR(title, INSTR(title, ']') + 1) ELSE title END) WHERE title LIKE '[%k] %' OR title LIKE '[%M] %' OR title LIKE '[%] %';";
            cp.exec('sqlite3 "' + sumDb + '" "' + cleanQuery + '"', () => {});
        } catch (e) {}
    }
}

cleanAllTitles();
