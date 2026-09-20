'use strict';

// Lightweight DOM shim + behaviour checks for web/static/js/edit.js.
//
// The project has no browser test framework, so this harness implements only the
// DOM APIs edit.js touches and verifies its observable behaviour: unsaved-change
// detection, the saving state, duplicate-submit prevention, the beforeunload
// warning and the pageshow reset.
//
// Usage: node edit_dom_harness.js <path-to-edit.js>
// Exits non-zero on the first failure.

const fs = require('fs');
const vm = require('vm');

const realConsole = console;

const scriptPath = process.argv[2];
if (!scriptPath) {
    realConsole.error('usage: node edit_dom_harness.js <path-to-edit.js>');
    process.exit(2);
}

const code = fs.readFileSync(scriptPath, 'utf8');

class Element {
    constructor(id) {
        this.id = id;
        this.attributes = {};
        this.value = '';
        this.disabled = false;
        this.textContent = '';
        this.listeners = {};
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
    addEventListener(type, fn) {
        this.listeners[type] = fn;
    }
}

function makeEvent() {
    return {
        defaultPrevented: false,
        preventDefault() {
            this.defaultPrevented = true;
        },
        returnValue: undefined
    };
}

function buildEnvironment(initialContent, withForm) {
    const form = new Element('edit-form');
    const textarea = new Element('edit-content');
    const button = new Element('edit-save');
    const status = new Element('edit-status');

    textarea.value = initialContent;

    const windowObj = {
        listeners: {},
        addEventListener(type, fn) {
            this.listeners[type] = fn;
        }
    };

    const elements = {
        'edit-form': withForm ? form : null,
        'edit-content': textarea,
        'edit-save': button,
        'edit-status': status
    };

    const env = {
        document: {
            getElementById: (id) => elements[id] || null
        },
        window: windowObj
    };

    return { form, textarea, button, status, window: windowObj, env };
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
    realConsole.log('edit.js behaviour checks');

    // 1. Exports, initial state and listener registration.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        check('editPage is exported', !!page);
        check('hasUnsavedChanges is exported', typeof page.hasUnsavedChanges === 'function');
        check('onSubmit is exported', typeof page.onSubmit === 'function');
        check('onBeforeUnload is exported', typeof page.onBeforeUnload === 'function');
        check('onPageShow is exported', typeof page.onPageShow === 'function');

        check('no changes initially', page.hasUnsavedChanges() === false);
        check('form submit listener registered', typeof t.form.listeners.submit === 'function');
        check('beforeunload listener registered', typeof t.window.listeners.beforeunload === 'function');
        check('pageshow listener registered', typeof t.window.listeners.pageshow === 'function');
    }

    // 2. Change detection.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        t.textarea.value = 'changed';
        check('change is detected', page.hasUnsavedChanges() === true);

        t.textarea.value = 'original';
        check('reverting clears the change flag', page.hasUnsavedChanges() === false);
    }

    // 3. Submitting enters the saving state.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        page.onSubmit(makeEvent());

        check('submitting flag is set', page.isSubmitting() === true);
        check('save button is disabled', t.button.disabled === true);
        check('save button shows Saving...', t.button.textContent === 'Saving...');
        check('status shows Saving...', t.status.textContent === 'Saving...');
        check('form is aria-busy', t.form.getAttribute('aria-busy') === 'true');
    }

    // 4. A second submission is prevented while one is in flight.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        page.onSubmit(makeEvent());
        const second = makeEvent();
        page.onSubmit(second);

        check('duplicate submit is prevented', second.defaultPrevented === true);
    }

    // 5. beforeunload warns only for unsaved, non-submitting edits.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        const clean = makeEvent();
        check('no warning without changes', page.onBeforeUnload(clean) === undefined);
        check('no preventDefault without changes', clean.defaultPrevented === false);

        t.textarea.value = 'dirty';
        const dirty = makeEvent();
        const result = page.onBeforeUnload(dirty);
        check('warning returned for unsaved changes', result === '');
        check('preventDefault called for unsaved changes', dirty.defaultPrevented === true);
        check('returnValue set for unsaved changes', dirty.returnValue === '');

        page.onSubmit(makeEvent());
        const during = makeEvent();
        check('no warning while submitting', page.onBeforeUnload(during) === undefined);
    }

    // 6. pageshow restores the button after a failed save / back navigation.
    {
        const t = buildEnvironment('original', true);
        load(t.env);
        const page = t.window.editPage;

        page.onSubmit(makeEvent());
        page.onPageShow();

        check('submitting flag is cleared', page.isSubmitting() === false);
        check('save button is re-enabled', t.button.disabled === false);
        check('save button label is restored', t.button.textContent === 'Save');
        check('status is cleared', t.status.textContent === '');
        check('aria-busy is removed', t.form.getAttribute('aria-busy') === undefined);
    }

    // 7. Missing form: the script must not throw and must not export.
    {
        const t = buildEnvironment('original', false);
        let threw = false;
        try {
            load(t.env);
        } catch (err) {
            threw = true;
        }
        check('missing form is safe', threw === false);
        check('nothing is exported without a form', t.window.editPage === undefined);
    }

    realConsole.log();
    if (failures > 0) {
        realConsole.log('FAILED: ' + failures + ' of ' + checks + ' checks');
        process.exit(1);
    }
    realConsole.log('ALL ' + checks + ' CHECKS PASSED');
}

main();
