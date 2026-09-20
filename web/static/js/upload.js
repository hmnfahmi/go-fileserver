(function () {
    const uploadForm = document.getElementById("upload-form");
    const uploadZone = document.getElementById("upload-zone");
    const fileInput = document.getElementById("upload-file");
    const selectedFile = document.getElementById("upload-selected");
    const warning = document.getElementById("upload-warning");

    if (!uploadForm || !uploadZone || !fileInput) {
        return;
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
        if (!fileInput.files || fileInput.files.length === 0) {
            selectedFile.textContent = "";
            selectedFile.classList.remove("show");
            return;
        }

        const files = Array.from(fileInput.files);

        const list = files
            .map(function (file) {
                return `${file.name} (${formatFileSize(file.size)})`;
            })
            .join(", ");

        selectedFile.textContent =
            `${files.length} file(s) selected: ${list}`;

        selectedFile.classList.add("show");

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

        if (!fileInput.files || fileInput.files.length === 0) {
            showWarning("Please select at least one file before uploading.");
            return;
        }

        hideWarning();

        const submitButton =
            uploadForm.querySelector("button[type='submit']");

        submitButton.disabled = true;
        submitButton.textContent = "Uploading...";

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
            showWarning(
                "Upload failed. Please check the server connection."
            );

            console.error("[UPLOAD]", error);

        } finally {
            submitButton.disabled = false;
            submitButton.textContent = "Upload";
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
            selectedFile.textContent = messages.join("\n");
            selectedFile.classList.add("show");
        }

        if (
            (!result.conflicts || result.conflicts.length === 0) &&
            (!result.failed || result.failed.length === 0)
        ) {
            window.location.reload();
        }
    }
})();