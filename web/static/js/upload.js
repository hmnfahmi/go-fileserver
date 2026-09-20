(function () {
    const uploadForm = document.getElementById("upload-form");
    const uploadZone = document.getElementById("upload-zone");
    const fileInput = document.getElementById("upload-file");
    const selectedFile = document.getElementById("upload-selected");
    const warning = document.getElementById("upload-warning");

    if (!uploadForm || !uploadZone || !fileInput) {
        return;
    }

    // isUploading guards against a second submission while a request is in
    // flight, which a disabled button alone does not prevent (an implicit
    // submit via the Enter key can still fire the submit event).
    let isUploading = false;

    function getSubmitButton() {
        return uploadForm.querySelector("button[type='submit']");
    }

    function selectionSummary() {
        if (!fileInput.files || fileInput.files.length === 0) {
            return "";
        }

        const files = Array.from(fileInput.files);

        const list = files
            .map(function (file) {
                return `${file.name} (${formatFileSize(file.size)})`;
            })
            .join(", ");

        return `${files.length} file(s) selected: ${list}`;
    }

    function setStatus(text) {
        selectedFile.textContent = text;
        selectedFile.classList.toggle("show", text !== "");
    }

    // setUploading toggles every piece of transient upload feedback: the button
    // label/disabled state, the form's aria-busy flag and the visible status
    // text. It is always called with active=false from the submit handler's
    // finally block so the UI is restored after success and failure alike.
    function setUploading(active, count) {
        const button = getSubmitButton();

        if (active) {
            uploadForm.setAttribute("aria-busy", "true");

            if (button) {
                button.disabled = true;
                button.textContent = "Uploading...";
            }

            setStatus(`Uploading ${count} file(s)...`);
            return;
        }

        uploadForm.removeAttribute("aria-busy");

        if (button) {
            button.disabled = false;
            button.textContent = "Upload";
        }
    }

    function showWarning(message) {
        warning.textContent = message;
        warning.classList.add("show");
    }

    function hideWarning() {
        warning.textContent = "";
        warning.classList.remove("show");
    }

    function formatFileSize(size) {
        if (size < 1024) {
            return `${size} B`;
        }

        if (size < 1024 * 1024) {
            return `${(size / 1024).toFixed(2)} KB`;
        }

        if (size < 1024 * 1024 * 1024) {
            return `${(size / (1024 * 1024)).toFixed(2)} MB`;
        }

        return `${(size / (1024 * 1024 * 1024)).toFixed(2)} GB`;
    }

    function updateSelectedFiles() {
        const summary = selectionSummary();

        if (summary === "") {
            setStatus("");
            return;
        }

        setStatus(summary);
        hideWarning();
    }

    function setDroppedFiles(files) {
        if (!files || files.length === 0) {
            return;
        }

        const dataTransfer = new DataTransfer();

        Array.from(files).forEach(function (file) {
            dataTransfer.items.add(file);
        });

        fileInput.files = dataTransfer.files;

        updateSelectedFiles();
    }

    uploadZone.addEventListener("click", function () {
        fileInput.click();
    });

    // The zone is a focusable role="button"; native buttons activate on Enter
    // and Space, so mirror that for keyboard users.
    uploadZone.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " " || event.key === "Spacebar") {
            event.preventDefault();
            fileInput.click();
        }
    });

    fileInput.addEventListener("change", function () {
        updateSelectedFiles();
    });

    uploadZone.addEventListener("dragenter", function (event) {
        event.preventDefault();

        uploadZone.classList.add("dragover");
    });

    uploadZone.addEventListener("dragover", function (event) {
        event.preventDefault();

        uploadZone.classList.add("dragover");
    });

    uploadZone.addEventListener("dragleave", function (event) {
        event.preventDefault();

        if (!uploadZone.contains(event.relatedTarget)) {
            uploadZone.classList.remove("dragover");
        }
    });

    uploadZone.addEventListener("drop", function (event) {
        event.preventDefault();

        uploadZone.classList.remove("dragover");

        setDroppedFiles(event.dataTransfer.files);
    });

    uploadForm.addEventListener("submit", async function (event) {
        event.preventDefault();

        if (isUploading) {
            return;
        }

        if (!fileInput.files || fileInput.files.length === 0) {
            showWarning("Please select at least one file before uploading.");
            return;
        }

        hideWarning();

        const fileCount = fileInput.files.length;

        isUploading = true;
        setUploading(true, fileCount);

        try {
            const formData = new FormData(uploadForm);

            const response = await fetch(
                uploadForm.action,
                {
                    method: "POST",
                    body: formData,
                    headers: {
                        "Accept": "application/json"
                    }
                }
            );

            const result = await response.json();

            handleUploadResult(result);

        } catch (error) {
            setStatus(selectionSummary());
            showWarning(
                "Upload failed. Please check the server connection."
            );

            console.error("[UPLOAD]", error);

        } finally {
            isUploading = false;
            setUploading(false);
        }
    });

    function handleUploadResult(result) {
        const messages = [];

        if (result.uploaded && result.uploaded.length > 0) {
            messages.push(
                `✓ ${result.uploaded.length} file(s) uploaded successfully.`
            );
        }

        if (result.conflicts && result.conflicts.length > 0) {
            messages.push(
                `⚠ ${result.conflicts.length} file(s) already exist.`
            );

            result.conflicts.forEach(function (fileName) {
                messages.push(`• ${fileName}`);
            });
        }

        if (result.failed && result.failed.length > 0) {
            messages.push(
                `✕ ${result.failed.length} file(s) failed.`
            );

            result.failed.forEach(function (file) {
                messages.push(
                    `• ${file.name}: ${file.message}`
                );
            });
        }

        if (messages.length > 0) {
            setStatus(messages.join("\n"));
        }

        if (
            (!result.conflicts || result.conflicts.length === 0) &&
            (!result.failed || result.failed.length === 0)
        ) {
            window.location.reload();
        }
    }
})();