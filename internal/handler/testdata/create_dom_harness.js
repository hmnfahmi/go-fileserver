'use strict';

// Lightweight DOM shim + behaviour checks for web/static/js/create.js.
//
// The project has no browser test framework, so this harness implements only the
// handful of DOM APIs create.js touches and verifies its observable behaviour:
// opening the correct native dialog, clearing and focusing the name field,
// closing any open dropdown, closing the dialog, and the no-showModal fallback.
//
// Usage: node create_dom_harness.js <path-to-create.js>
// Exits non-zero on the first failure.

const fs = require('fs');
const vm = require('vm');

const realConsole = console;

const scriptPath = process.argv[2];
if (!scriptPath) {
    realConsole.error('usage: node create_dom_harness.js <path-to-create.js>');
    process.exit(2);
}

const code = fs.readFileSync(scriptPath, 'utf8');

class ClassList {
    constructor() {
        this.set = new Set();
    }
    add(...names) {
        names.forEach((name) => this.set.add(name));
    }
    remove(...names) {
        names.forEach((name) => this.set.delete(name));
    }
    contains(name) {
        return this.set.has(name);
    }
}

class Element {
    constructor(id) {
        this.id = id;
        this.classList = new ClassList();
        this.attributes = {};
        this.value = '';
        this.focused = 0;
        this.shown = 0;
        this.closed = 0;
        this.nameInput = null;
    }
    setAttribute(name, value) {
        this.attributes[name] = String(value);
    }
    removeAttribute(name) {
        delete this.attributes[name];
    }
    getAttribute(name) {
        return this.attributes[name];
    }
    focus() {
        this.focused += 1;
    }
    showModal() {
        this.shown += 1;
    }
    close() {
        this.closed += 1;
    }
    querySelector() {
        return this.nameInput;
    }
}

function buildEnvironment(useFallback) {
    const folder = new Element('new-folder-dialog');
    const file = new Element('new-file-dialog');

    const folderInput = new Element('new-folder-name');
    folderInput.value = 'stale-folder';
    const fileInput = new Element('new-file-name');
    fileInput.value = 'stale-file';

    folder.nameInput = folderInput;
    file.nameInput = fileInput;

    if (useFallback) {
        folder.showModal = undefined;
        folder.close = undefined;
    }

    const menu = new Element('menu');
    menu.classList.add('show');

    const dialogs = {
        'new-folder-dialog': folder,
        'new-file-dialog': file,
    };

    const env = {
        document: {
            getElementById: (id) => dialogs[id] || null,
            querySelectorAll: (selector) =>
                selector === '.action-dropdown.show' ? [menu] : [],
        },
        window: {},
    };

    return {
        folder: folder,
        file: file,
        folderInput: folderInput,
        fileInput: fileInput,
        menu: menu,
        env: env,
    };
}

function load(env) {
    Object.assign(globalThis, env);
    vm.runInThisContext(code, { filename: scriptPath });
}

let failures = 0;
let checks = 0;

function check(name, condition, detail) {
    checks += 1;
    if (condition) {
        realConsole.log('  PASS  ' + name);
        return;
    }
    failures += 1;
    realConsole.log('  FAIL  ' + name + (detail ? ' -- ' + detail : ''));
}

function main() {
    realConsole.log('create.js behaviour checks');

    // 1. Opening the folder dialog shows it, clears the field, focuses it and
    //    closes any open dropdown.
    {
        const t = buildEnvironment(false);
        load(t.env);

        check(
            'openCreateDialog is exported',
            typeof t.env.window.openCreateDialog === 'function'
        );
        check(
            'closeCreateDialog is exported',
            typeof t.env.window.closeCreateDialog === 'function'
        );

        t.env.window.openCreateDialog('folder');

        check('folder dialog is shown', t.folder.shown === 1, 'shown=' + t.folder.shown);
        check('file dialog stays closed', t.file.shown === 0);
        check('name field is cleared', t.folderInput.value === '', 'value=' + t.folderInput.value);
        check('name field is focused', t.folderInput.focused === 1);
        check('open dropdown is closed', t.menu.classList.contains('show') === false);
    }

    // 2. Opening the file dialog targets the other dialog.
    {
        const t = buildEnvironment(false);
        load(t.env);

        t.env.window.openCreateDialog('file');

        check('file dialog is shown', t.file.shown === 1, 'shown=' + t.file.shown);
        check('folder dialog stays closed', t.folder.shown === 0);
        check('file name field is cleared', t.fileInput.value === '');
        check('file name field is focused', t.fileInput.focused === 1);
    }

    // 3. Closing a dialog by id.
    {
        const t = buildEnvironment(false);
        load(t.env);

        t.env.window.closeCreateDialog('new-folder-dialog');
        check('dialog is closed', t.folder.closed === 1, 'closed=' + t.folder.closed);
    }

    // 4. Closing an unknown id must not throw.
    {
        const t = buildEnvironment(false);
        load(t.env);

        let threw = false;
        try {
            t.env.window.closeCreateDialog('missing-dialog');
        } catch (err) {
            threw = true;
        }
        check('closing an unknown dialog is safe', threw === false);
    }

    // 5. Fallback for engines without showModal/close.
    {
        const t = buildEnvironment(true);
        load(t.env);

        let threw = false;
        try {
            t.env.window.openCreateDialog('folder');
        } catch (err) {
            threw = true;
        }
        check('fallback open does not throw', threw === false);
        check(
            'fallback open sets the open attribute',
            t.folder.getAttribute('open') === ''
        );

        t.env.window.closeCreateDialog('new-folder-dialog');
        check(
            'fallback close removes the open attribute',
            t.folder.getAttribute('open') === undefined
        );
    }

    realConsole.log();
    if (failures > 0) {
        realConsole.log('FAILED: ' + failures + ' of ' + checks + ' checks');
        process.exit(1);
    }
    realConsole.log('ALL ' + checks + ' CHECKS PASSED');
}

main();
