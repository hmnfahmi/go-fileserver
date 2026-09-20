'use strict';

// Lightweight DOM shim + behaviour checks for web/static/js/upload.js.
//
// The project has no browser test framework, so this harness implements only
// the handful of DOM APIs upload.js touches and exercises its observable
// behaviour: loading activation, duplicate-submission prevention, cleanup on
// success and failure, multi-file selection and drag & drop.
//
// Usage: node upload_dom_harness.js <path-to-upload.js>
// Exits non-zero on the first failure.

const fs = require('fs');
const vm = require('vm');

// Capture the real console before the shim replaces globalThis.console, so the
// harness's own output is never swallowed by the loaded script's no-op console.
const realConsole = console;

const uploadPath = process.argv[2];
if (!uploadPath) {
    realConsole.error('usage: node upload_dom_harness.js <path-to-upload.js>');
    process.exit(2);
}

const code = fs.readFileSync(uploadPath, 'utf8');

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
    toggle(name, force) {
        const want = force === undefined ? !this.set.has(name) : Boolean(force);
        if (want) {
            this.set.add(name);
        } else {
            this.set.delete(name);
        }
        return want;
    }
}

class Element {
    constructor(id) {
        this.id = id;
        this.classList = new ClassList();
        this.listeners = {};
        this.attributes = {};
        this.textContent = '';
        this.disabled = false;
        this.files = null;
        this.action = '';
        this.clicked = 0;
        this.submitButton = null;
    }
    addEventListener(type, fn) {
        (this.listeners[type] = this.listeners[type] || []).push(fn);
    }
    dispatch(type, event) {
        (this.listeners[type] || []).forEach((fn) => fn(event || {}));
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
    click() {
        this.clicked += 1;
    }
    contains() {
        return false;
    }
    querySelector(selector) {
        if (selector && selector.indexOf('submit') !== -1) {
            return this.submitButton;
        }
        return null;
    }
}

class FakeDataTransfer {
    constructor() {
        this._files = [];
        this.items = {
            add: (file) => {
                this._files.push(file);
            },
        };
    }
    get files() {
        return this._files;
    }
}

function makeFile(name, size) {
    return { name: name, size: size };
}

function deferred() {
    let resolve;
    let reject;
    const promise = new Promise((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise: promise, resolve: resolve, reject: reject };
}

function tick() {
    return new Promise((resolve) => setImmediate(resolve));
}

async function settle() {
    await tick();
    await tick();
    await tick();
}

// buildEnvironment creates fresh elements and the globals upload.js expects.
function buildEnvironment() {
    const ids = ['upload-form', 'upload-zone', 'upload-file', 'upload-selected', 'upload-warning'];
    const elements = {};
    ids.forEach((id) => {
        elements[id] = new Element(id);
    });

    const submitButton = new Element('upload-submit');
    submitButton.textContent = 'Upload';
    elements['upload-form'].submitButton = submitButton;
    elements['upload-form'].action = '/upload?path=';

    const state = { reloads: 0 };

    const env = {
        document: {
            getElementById: (id) => elements[id] || null,
        },
        window: {
            location: {
                reload: () => {
                    state.reloads += 1;
                },
            },
        },
        DataTransfer: FakeDataTransfer,
        FormData: class FormData {
            constructor(form) {
                this.form = form;
            }
        },
        fetch: null,
        console: {
            log: () => {},
            error: () => {},
        },
    };

    return { elements: elements, submitButton: submitButton, state: state, env: env };
}

function load(elements, env) {
    Object.assign(globalThis, env);
    vm.runInThisContext(code, { filename: uploadPath });
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

function contains(text, needle) {
    return typeof text === 'string' && text.indexOf(needle) !== -1;
}

function submitForm(form) {
    form.dispatch('submit', { preventDefault: () => {} });
}

async function main() {
    realConsole.log('upload.js behaviour checks');

    // 1. Submitting with no files warns and never calls fetch.
    {
        const { elements, env } = buildEnvironment();
        let calls = 0;
        env.fetch = () => {
            calls += 1;
            return Promise.resolve({ json: async () => ({}) });
        };
        load(elements, env);

        submitForm(elements['upload-form']);
        await settle();

        check('no-file submit does not fetch', calls === 0, 'calls=' + calls);
        check(
            'no-file submit shows a warning',
            elements['upload-warning'].classList.contains('show') &&
                contains(elements['upload-warning'].textContent, 'at least one file')
        );
    }

    // 2. A normal multi-file upload activates loading, fetches once, reloads on
    //    success and restores the UI.
    {
        const { elements, submitButton, state, env } = buildEnvironment();
        const pending = deferred();
        let calls = 0;
        env.fetch = () => {
            calls += 1;
            return pending.promise;
        };
        load(elements, env);

        const input = elements['upload-file'];
        input.files = [makeFile('a.txt', 10), makeFile('b.txt', 2048)];
        input.dispatch('change', {});

        check(
            'multi-file selection is summarised',
            contains(elements['upload-selected'].textContent, '2 file(s) selected') &&
                contains(elements['upload-selected'].textContent, 'b.txt')
        );

        submitForm(elements['upload-form']);
        await settle();

        check('upload activates the loading button', submitButton.disabled === true);
        check('upload changes the button label', submitButton.textContent === 'Uploading...');
        check(
            'upload sets aria-busy',
            elements['upload-form'].getAttribute('aria-busy') === 'true'
        );
        check(
            'upload shows progress text',
            contains(elements['upload-selected'].textContent, 'Uploading 2 file(s)')
        );
        check('upload issues exactly one request', calls === 1, 'calls=' + calls);

        pending.resolve({
            json: async () => ({ uploaded: ['a.txt', 'b.txt'], conflicts: [], failed: [] }),
        });
        await settle();

        check('success reloads the page', state.reloads === 1, 'reloads=' + state.reloads);
        check('success restores the button', submitButton.disabled === false);
        check('success restores the label', submitButton.textContent === 'Upload');
        check(
            'success clears aria-busy',
            elements['upload-form'].getAttribute('aria-busy') === undefined
        );
    }

    // 3. A second submission while the first is in flight is ignored.
    {
        const { elements, env } = buildEnvironment();
        const pending = deferred();
        let calls = 0;
        env.fetch = () => {
            calls += 1;
            return pending.promise;
        };
        load(elements, env);

        elements['upload-file'].files = [makeFile('a.txt', 1)];
        submitForm(elements['upload-form']);
        await settle();

        submitForm(elements['upload-form']);
        submitForm(elements['upload-form']);
        await settle();

        check('duplicate submissions are prevented', calls === 1, 'calls=' + calls);

        pending.resolve({ json: async () => ({ uploaded: ['a.txt'], conflicts: [], failed: [] }) });
        await settle();
    }

    // 4. A failed request clears loading, restores the UI and warns.
    {
        const { elements, submitButton, env } = buildEnvironment();
        env.fetch = () => Promise.reject(new Error('network down'));
        load(elements, env);

        elements['upload-file'].files = [makeFile('a.txt', 1)];
        submitForm(elements['upload-form']);
        await settle();

        check(
            'failure shows a warning',
            elements['upload-warning'].classList.contains('show') &&
                contains(elements['upload-warning'].textContent, 'Upload failed')
        );
        check('failure restores the button', submitButton.disabled === false);
        check('failure restores the label', submitButton.textContent === 'Upload');
        check(
            'failure clears aria-busy',
            elements['upload-form'].getAttribute('aria-busy') === undefined
        );
        check(
            'failure removes the uploading text',
            !contains(elements['upload-selected'].textContent, 'Uploading')
        );
    }

    // 5. Conflicts do not reload, show a message and restore the UI.
    {
        const { elements, submitButton, state, env } = buildEnvironment();
        env.fetch = () =>
            Promise.resolve({
                json: async () => ({ uploaded: [], conflicts: ['dup.txt'], failed: [] }),
            });
        load(elements, env);

        elements['upload-file'].files = [makeFile('dup.txt', 1)];
        submitForm(elements['upload-form']);
        await settle();

        check('conflict does not reload', state.reloads === 0, 'reloads=' + state.reloads);
        check(
            'conflict reports the file',
            contains(elements['upload-selected'].textContent, 'already exist') &&
                contains(elements['upload-selected'].textContent, 'dup.txt')
        );
        check('conflict restores the button', submitButton.disabled === false);
    }

    // 6. Drag & drop still populates the file input and updates the summary.
    {
        const { elements, env } = buildEnvironment();
        env.fetch = () => Promise.resolve({ json: async () => ({}) });
        load(elements, env);

        const transfer = new FakeDataTransfer();
        transfer.items.add(makeFile('dropped.txt', 5));
        transfer.items.add(makeFile('second.txt', 7));

        elements['upload-zone'].dispatch('drop', {
            preventDefault: () => {},
            dataTransfer: transfer,
        });

        check(
            'drop populates the file input',
            elements['upload-file'].files && elements['upload-file'].files.length === 2,
            'files=' + (elements['upload-file'].files || []).length
        );
        check(
            'drop updates the summary',
            contains(elements['upload-selected'].textContent, 'dropped.txt')
        );
        check(
            'drop clears the dragover class',
            !elements['upload-zone'].classList.contains('dragover')
        );
    }

    // 7. The upload zone is keyboard-activatable.
    {
        const { elements, env } = buildEnvironment();
        env.fetch = () => Promise.resolve({ json: async () => ({}) });
        load(elements, env);

        let prevented = 0;
        elements['upload-zone'].dispatch('keydown', {
            key: 'Enter',
            preventDefault: () => {
                prevented += 1;
            },
        });

        check('Enter activates the upload zone', elements['upload-file'].clicked === 1);
        check('Enter is preventDefaulted', prevented === 1);
    }

    realConsole.log('');
    realConsole.log(failures === 0 ? 'ALL ' + checks + ' CHECKS PASSED' : failures + '/' + checks + ' CHECKS FAILED');
    process.exit(failures === 0 ? 0 : 1);
}

main().catch((error) => {
    realConsole.error('harness error:', error);
    process.exit(1);
});
