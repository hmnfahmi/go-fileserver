(function () {
    'use strict';

    var form = document.getElementById('edit-form');
    var textarea = document.getElementById('edit-content');
    var button = document.getElementById('edit-save');
    var status = document.getElementById('edit-status');

    if (!form || !textarea) {
        return;
    }

    // initialValue is the content as the server rendered it. Everything else is
    // derived from it; the server remains the authority for what is saved.
    var initialValue = textarea.value;

    // submitting guards against a second submission while a request is in
    // flight. A disabled button alone is not enough: an implicit submit (Enter
    // key) can still fire the submit event.
    var submitting = false;

    function hasUnsavedChanges() {
        return textarea.value !== initialValue;
    }

    // setSaving toggles the transient save feedback: the form's aria-busy flag,
    // the button label/disabled state and the visible status text.
    function setSaving(active) {
        if (active) {
            form.setAttribute('aria-busy', 'true');

            if (button) {
                button.disabled = true;
                button.textContent = 'Saving...';
            }

            if (status) {
                status.textContent = 'Saving...';
            }

            return;
        }

        form.removeAttribute('aria-busy');

        if (button) {
            button.disabled = false;
            button.textContent = 'Save';
        }

        if (status) {
            status.textContent = '';
        }
    }

    function onSubmit(event) {
        if (submitting) {
            if (event && typeof event.preventDefault === 'function') {
                event.preventDefault();
            }
            return;
        }

        submitting = true;
        setSaving(true);
    }

    function onBeforeUnload(event) {
        if (submitting || !hasUnsavedChanges()) {
            return undefined;
        }

        if (event) {
            event.preventDefault();
            event.returnValue = '';
        }

        return '';
    }

    // pageshow fires on the first load and again when the page is restored from
    // the back/forward cache. Resetting here restores the Save button after the
    // browser returns from a failed save (the server error page) or from a
    // cancelled navigation.
    function onPageShow() {
        submitting = false;
        setSaving(false);
    }

    form.addEventListener('submit', onSubmit);
    window.addEventListener('beforeunload', onBeforeUnload);
    window.addEventListener('pageshow', onPageShow);

    // Exposed for the lightweight DOM harness; it is also harmless in a browser.
    window.editPage = {
        hasUnsavedChanges: hasUnsavedChanges,
        isSubmitting: function () {
            return submitting;
        },
        setSaving: setSaving,
        onSubmit: onSubmit,
        onBeforeUnload: onBeforeUnload,
        onPageShow: onPageShow
    };
})();
