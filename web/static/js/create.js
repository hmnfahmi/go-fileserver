(function () {
    'use strict';

    function dialogFor(kind) {
        return document.getElementById(
            kind === 'folder' ? 'new-folder-dialog' : 'new-file-dialog'
        );
    }

    // openCreateDialog shows the create form for "folder" or "file". The dialog
    // is a native <dialog>, so no modal library is needed; older engines fall
    // back to the open attribute.
    function openCreateDialog(kind) {
        var dialog = dialogFor(kind);
        if (!dialog) {
            return;
        }

        // Close any open New/row dropdown so it does not sit above the dialog.
        document
            .querySelectorAll('.action-dropdown.show')
            .forEach(function (menu) {
                menu.classList.remove('show');
            });

        var input = dialog.querySelector('input[name="name"]');
        if (input) {
            input.value = '';
        }

        if (typeof dialog.showModal === 'function') {
            dialog.showModal();
        } else {
            dialog.setAttribute('open', '');
        }

        if (input) {
            input.focus();
        }
    }

    function closeCreateDialog(id) {
        var dialog = document.getElementById(id);
        if (!dialog) {
            return;
        }

        if (typeof dialog.close === 'function') {
            dialog.close();
        } else {
            dialog.removeAttribute('open');
        }
    }

    window.openCreateDialog = openCreateDialog;
    window.closeCreateDialog = closeCreateDialog;
})();
