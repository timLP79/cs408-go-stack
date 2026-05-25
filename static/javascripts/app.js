document.addEventListener("DOMContentLoaded", function () {
    initCatalogFilter();
    initStaffManagement();
    initPatronManagement();
    initBookDetail();
    initBookForm();
    initAddCopyModal();
});

// initAddCopyModal wires the source-radio toggle inside the Add Copy
// modal on the Manage Copies page. The library section (bulk count
// input) and scan section (format + barcode inputs) are mutually
// exclusive based on the selected source radio. Inputs in the hidden
// section are also "disabled" so their values are not submitted with
// the form, preventing the server from seeing leftover state from the
// other branch.
function initAddCopyModal() {
    var modal = document.getElementById("addCopyModal");
    if (!modal) return;

    var librarySection = modal.querySelector(".add-copy-source-library");
    var scanSection = modal.querySelector(".add-copy-source-scan");
    var radios = modal.querySelectorAll('input[name="source"]');
    if (!librarySection || !scanSection || radios.length === 0) return;

    function applySource(value) {
        var isLibrary = value === "library";
        librarySection.classList.toggle("d-none", !isLibrary);
        scanSection.classList.toggle("d-none", isLibrary);
        librarySection.querySelectorAll("input, select").forEach(function (el) {
            el.disabled = !isLibrary;
        });
        scanSection.querySelectorAll("input, select").forEach(function (el) {
            el.disabled = isLibrary;
        });
    }

    radios.forEach(function (radio) {
        radio.addEventListener("change", function () {
            if (radio.checked) applySource(radio.value);
        });
    });

    // Initial state matches whichever radio is checked in the markup.
    var initial = modal.querySelector('input[name="source"]:checked');
    applySource(initial ? initial.value : "library");
}

function initCatalogFilter() {
    var searchInput = document.getElementById("catalog-search");
    var genreFilter = document.getElementById("genre-filter");
    var availableFilter = document.getElementById("available-filter");
    var bookCount = document.getElementById("book-count");
    var cards = document.querySelectorAll(".book-card");

    if (!searchInput || !cards.length) return;

    var genres = [];
    cards.forEach(function (card) {
        var genre = card.getAttribute("data-genre");
        if (genre && genres.indexOf(genre) === -1) {
            genres.push(genre);
        }
    });
    genres.sort();
    genres.forEach(function (genre) {
        var option = document.createElement("option");
        option.value = genre;
        option.textContent = genre;
        genreFilter.appendChild(option);
    });

    function filterBooks() {
        var query = searchInput.value.toLowerCase();
        var genre = genreFilter.value;
        var onlyAvailable = availableFilter.checked;
        var visible = 0;

        cards.forEach(function (card) {
            var title = card.getAttribute("data-title").toLowerCase();
            var authors = card.getAttribute("data-authors").toLowerCase();
            var isbn = card.getAttribute("data-isbn").toLowerCase();
            var cardGenre = card.getAttribute("data-genre");
            var available = parseInt(card.getAttribute("data-available"), 10);

            var matchesSearch = !query || title.indexOf(query) !== -1 ||
                authors.indexOf(query) !== -1 || isbn.indexOf(query) !== -1;
            var matchesGenre = !genre || cardGenre === genre;
            var matchesAvailable = !onlyAvailable || available > 0;

            if (matchesSearch && matchesGenre && matchesAvailable) {
                card.style.display = "";
                visible++;
            } else {
                card.style.display = "none";
            }
        });

        bookCount.textContent = "Showing " + visible + " of " + cards.length + " books";
    }

    searchInput.addEventListener("input", filterBooks);
    genreFilter.addEventListener("change", filterBooks);
    availableFilter.addEventListener("change", filterBooks);
}

function initStaffManagement() {
    var editModal = document.getElementById("editStaffModal");
    var deleteModal = document.getElementById("deleteStaffModal");
    var resetModal = document.getElementById("resetPasswordModal");
    if (!editModal && !deleteModal && !resetModal) return;

    // Populate Edit modal from clicked row's data attributes
    if (editModal) {
        var editForm = document.getElementById("editStaffForm");
        var editUsername = document.getElementById("edit-username");
        var editRole = document.getElementById("edit-role");
        var editNote = document.getElementById("edit-role-note");

        document.querySelectorAll(".edit-btn").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var id = btn.getAttribute("data-user-id");
                var username = btn.getAttribute("data-username");
                var role = btn.getAttribute("data-role");
                var isSelf = btn.getAttribute("data-is-self") === "true";
                var isLastAdmin = btn.getAttribute("data-is-last-admin") === "true";

                editForm.action = "/staff/" + id + "/edit";
                editUsername.value = username;
                editRole.value = role;

                Array.from(editRole.options).forEach(function (opt) {
                    opt.disabled = false;
                });
                editRole.disabled = false;
                editNote.textContent = "";

                if (isSelf) {
                    editRole.disabled = true;
                    editNote.textContent = "You cannot change your own role.";
                } else if (isLastAdmin) {
                    Array.from(editRole.options).forEach(function (opt) {
                        if (opt.value === "staff") opt.disabled = true;
                    });
                    editNote.textContent = "This is the last admin account. Create another admin before demoting.";
                }
            });
        });
    }

    // Type-to-confirm delete
    if (deleteModal) {
        var deleteForm = document.getElementById("deleteStaffForm");
        var deleteTargetName = document.getElementById("delete-target-name");
        var deleteTargetRole = document.getElementById("delete-target-role");
        var deleteInput = document.getElementById("delete-confirm-input");
        var deleteBtn = document.getElementById("delete-confirm-btn");
        var expectedUsername = "";

        document.querySelectorAll(".delete-btn").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var id = btn.getAttribute("data-user-id");
                expectedUsername = btn.getAttribute("data-username");
                var role = btn.getAttribute("data-role");

                deleteForm.action = "/staff/" + id + "/delete";
                deleteTargetName.textContent = expectedUsername;
                deleteTargetRole.textContent = role;
                deleteInput.value = "";
                deleteBtn.disabled = true;
            });
        });

        deleteInput.addEventListener("input", function () {
            deleteBtn.disabled = deleteInput.value !== expectedUsername;
        });

        deleteModal.addEventListener("hidden.bs.modal", function () {
            deleteInput.value = "";
            deleteBtn.disabled = true;
        });
    }

    // Reset Password modal
    if (resetModal) {
        var resetForm = document.getElementById("resetPasswordForm");
        var resetTargetName = document.getElementById("reset-target-name");
        var resetPassword = document.getElementById("reset-password");
        var resetPasswordConfirm = document.getElementById("reset-password-confirm");

        document.querySelectorAll(".reset-btn").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var id = btn.getAttribute("data-user-id");
                var username = btn.getAttribute("data-username");

                resetForm.action = "/staff/" + id + "/password";
                resetTargetName.textContent = username;
                resetPassword.value = "";
                resetPasswordConfirm.value = "";
            });
        });

        resetModal.addEventListener("hidden.bs.modal", function () {
            resetPassword.value = "";
            resetPasswordConfirm.value = "";
        });
    }

    attachFormValidation(document.getElementById("addStaffForm"), {
        password: "#add-password",
        confirm: "#add-password-confirm",
        modal: document.getElementById("addStaffModal"),
    });
    attachFormValidation(document.getElementById("editStaffForm"), {
        modal: editModal,
    });
    attachFormValidation(document.getElementById("resetPasswordForm"), {
        password: "#reset-password",
        confirm: "#reset-password-confirm",
        modal: resetModal,
    });
}

function initPatronManagement() {
    var editModal = document.getElementById("editPatronModal");
    var deleteModal = document.getElementById("deletePatronModal");
    var addModal = document.getElementById("addPatronModal");
    if (!editModal && !deleteModal && !addModal) return;

    // Populate Edit modal from the clicked row's data attributes.
    // Mirrors initStaffManagement but without the role/is-self logic
    // (patron edit only covers name/email/phone; username is not
    // editable per the #21 design).
    if (editModal) {
        var editForm = document.getElementById("editPatronForm");
        var editName = document.getElementById("edit-patron-name");
        var editEmail = document.getElementById("edit-patron-email");
        var editPhone = document.getElementById("edit-patron-phone");
        var editAddress = document.getElementById("edit-patron-address");

        document.querySelectorAll(".patron-edit-btn").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var id = btn.getAttribute("data-patron-id");
                editForm.action = "/patrons/" + id + "/edit";
                editName.value = btn.getAttribute("data-patron-name") || "";
                editEmail.value = btn.getAttribute("data-patron-email") || "";
                editPhone.value = btn.getAttribute("data-patron-phone") || "";
                if (editAddress) editAddress.value = btn.getAttribute("data-patron-address") || "";
            });
        });
    }

    // Type-to-confirm delete. Same pattern as initStaffManagement;
    // patron name is the token the admin must type to enable submit.
    if (deleteModal) {
        var deleteForm = document.getElementById("deletePatronForm");
        var deleteTarget = document.getElementById("delete-patron-target-name");
        var deleteInput = document.getElementById("delete-patron-confirm-input");
        var deleteBtn = document.getElementById("delete-patron-confirm-btn");
        var expectedName = "";

        document.querySelectorAll(".patron-delete-btn").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var id = btn.getAttribute("data-patron-id");
                expectedName = btn.getAttribute("data-patron-name");
                deleteForm.action = "/patrons/" + id + "/delete";
                deleteTarget.textContent = expectedName;
                deleteInput.value = "";
                deleteBtn.disabled = true;
            });
        });

        deleteInput.addEventListener("input", function () {
            deleteBtn.disabled = deleteInput.value !== expectedName;
        });

        deleteModal.addEventListener("hidden.bs.modal", function () {
            deleteInput.value = "";
            deleteBtn.disabled = true;
        });
    }

    // Bootstrap live validation on all three patron forms. Add form
    // has password + confirm pair (same as staff Add), Edit form has
    // no password fields, Delete form has none either (type-to-
    // confirm is handled separately above).
    attachFormValidation(document.getElementById("addPatronForm"), {
        password: "#add-patron-password",
        confirm: "#add-patron-password-confirm",
        modal: addModal,
    });
    attachFormValidation(document.getElementById("editPatronForm"), {
        modal: editModal,
    });
}

function initBookDetail() {
    var deleteModal = document.getElementById("deleteBookModal");
    if (!deleteModal) return;

    var deleteForm = document.getElementById("deleteBookForm");
    var titleTarget = document.getElementById("delete-book-title");
    var confirmInput = document.getElementById("delete-book-confirm-input");
    var confirmBtn = document.getElementById("delete-book-confirm-btn");
    var expectedTitle = "";

    document.querySelectorAll('[data-bs-target="#deleteBookModal"]').forEach(function (btn) {
        btn.addEventListener("click", function () {
            var id = btn.getAttribute("data-book-id");
            expectedTitle = btn.getAttribute("data-book-title");
            deleteForm.action = "/books/" + id + "/delete";
            titleTarget.textContent = expectedTitle;
            confirmInput.value = "";
            confirmBtn.disabled = true;
        });
    });

    confirmInput.addEventListener("input", function () {
        confirmBtn.disabled = confirmInput.value !== expectedTitle;
    });

    deleteModal.addEventListener("hidden.bs.modal", function () {
        confirmInput.value = "";
        confirmBtn.disabled = true;
    });
}

function initBookForm() {
    var form = document.getElementById("bookForm");
    if (!form) return;

    // Open Library lookup: fetch metadata for the ISBN in the form,
    // prefill title/authors/year/publisher, and stage a cover URL in the
    // hidden cover_url field so the server can download it on submit.
    // The admin can still override any field before saving.
    var lookupBtn = document.getElementById("ol-lookup-btn");
    var isbnField = document.getElementById("book-isbn");
    var titleField = document.getElementById("book-title");
    var authorsField = document.getElementById("book-authors");
    var yearField = document.getElementById("book-year");
    var publisherField = document.getElementById("book-publisher");
    var descriptionField = document.getElementById("book-description");
    var coverUrlField = document.getElementById("cover-url");
    var coverPreview = document.getElementById("cover-preview");
    var coverUrlNote = document.getElementById("cover-url-note");

    function setStatus(msg, kind) {
        var existing = document.getElementById("ol-lookup-status");
        if (existing) existing.remove();
        if (!msg) return;
        var div = document.createElement("div");
        div.id = "ol-lookup-status";
        div.className = "form-text mt-2 " + (kind === "error" ? "text-danger" : "text-success");
        div.textContent = msg;
        lookupBtn.parentElement.appendChild(div);
    }

    if (lookupBtn && isbnField) {
        function applyOfflineHint() {
            lookupBtn.disabled = true;
            setStatus("Browser is offline. Lookup unavailable.", "error");
            var el = document.getElementById("ol-lookup-status");
            if (el) el.dataset.offlineHint = "1";
        }
        function clearOfflineHint() {
            lookupBtn.disabled = false;
            var el = document.getElementById("ol-lookup-status");
            // Only clear if the current status was our offline hint;
            // don't wipe a real lookup result the user is reading.
            if (el && el.dataset.offlineHint === "1") el.remove();
        }
        if (!navigator.onLine) applyOfflineHint();
        window.addEventListener("offline", applyOfflineHint);
        window.addEventListener("online", clearOfflineHint);

        lookupBtn.addEventListener("click", function () {
            var isbn = isbnField.value.replace(/[\s-]/g, "");
            if (!isbn) {
                setStatus("Enter an ISBN first.", "error");
                return;
            }
            setStatus("Looking up...", "info");
            lookupBtn.disabled = true;

            fetch("/api/openlibrary/isbn/" + encodeURIComponent(isbn), {
                headers: { "Accept": "application/json" }
            }).then(function (resp) {
                lookupBtn.disabled = false;
                if (resp.status === 503) {
                    // Covers offline_mode and external_sources_unavailable; both retry-able.
                    setStatus("External sources unavailable. Try again or fill in manually.", "error");
                    return null;
                }
                if (resp.status === 404) {
                    setStatus("No match in Open Library. Fill in manually.", "error");
                    return null;
                }
                if (resp.status === 400) {
                    setStatus("ISBN must be 10 or 13 characters.", "error");
                    return null;
                }
                if (!resp.ok) {
                    setStatus("Couldn't reach Open Library. Try again or fill in manually.", "error");
                    return null;
                }
                return resp.json();
            }).then(function (data) {
                if (!data) return;
                if (data.title) titleField.value = data.title;
                if (Array.isArray(data.authors) && data.authors.length) {
                    authorsField.value = data.authors.join(", ");
                }
                if (data.publish_year) yearField.value = data.publish_year;
                if (data.publisher) publisherField.value = data.publisher;
                // Description follows the same overwrite-on-OL-hit
                // semantics as title/authors/year/publisher above: if
                // OL returns a value, take it. A second consecutive
                // Lookup against a different ISBN must update every
                // prefilled field, including description.
                if (data.description && descriptionField) {
                    descriptionField.value = data.description;
                }
                if (data.cover_url) {
                    coverUrlField.value = data.cover_url;
                    if (coverPreview) coverPreview.src = data.cover_url;
                    if (coverUrlNote) coverUrlNote.style.display = "";
                } else if (coverUrlField.value) {
                    // OL has no cover for this ISBN, but a previous OL
                    // Lookup staged one in this session. Clear the staged
                    // URL and reset the preview to the placeholder so the
                    // previous lookup's cover doesn't visually carry over.
                    // An existing book-detail cover (cover_filename set in
                    // DB) is NOT reached here -- coverUrlField.value stays
                    // empty in edit mode unless an OL Lookup populated it,
                    // so the original /covers/<file> preview is preserved
                    // when OL has nothing to offer.
                    coverUrlField.value = "";
                    if (coverPreview) coverPreview.src = "/images/no-cover.svg";
                    if (coverUrlNote) coverUrlNote.style.display = "none";
                }
                var msg = data.cover_url
                    ? "Prefilled from Open Library. Review before saving."
                    : "Prefilled from Open Library (no cover available). Review before saving.";
                if (data.description_source === "googlebooks" || data.cover_source === "googlebooks") {
                    msg += " Some fields via Google Books.";
                }
                if (data.google_books_error) {
                    msg += " Google Books unavailable; showing Open Library data only.";
                }
                setStatus(msg, "success");
            }).catch(function () {
                lookupBtn.disabled = false;
                setStatus("Couldn't reach Open Library. Try again or fill in manually.", "error");
            });
        });
    }

    // Local cover-file preview so admins see what they selected without
    // submitting. Uploading a file also clears any staged OL cover_url so
    // the server routing (upload > URL > preserve existing) matches.
    var coverInput = document.getElementById("cover-file");
    if (coverInput) {
        coverInput.addEventListener("change", function () {
            var file = coverInput.files && coverInput.files[0];
            if (!file) return;
            if (coverUrlField) coverUrlField.value = "";
            if (coverUrlNote) coverUrlNote.style.display = "none";
            if (!coverPreview) return;
            var reader = new FileReader();
            reader.onload = function (e) {
                coverPreview.src = e.target.result;
            };
            reader.readAsDataURL(file);
        });
    }

    attachFormValidation(form, {});
}

// attachFormValidation wires Bootstrap 5 per-field live validation onto
// a form. Each non-hidden input/select gets input+blur listeners that
// toggle .is-invalid or .is-valid on the field based on its validity,
// so the red/green feedback and .invalid-feedback message appear as the
// user types. Empty fields stay neutral (no red until the user has
// interacted or submitted) so the modal does not nag on open. On submit,
// every field is forced through validation so untouched empty requireds
// also light up red. Server-side validation remains the source of truth;
// this is the client-side short-circuit so the user does not round-trip
// for client-detectable mistakes.
function attachFormValidation(form, options) {
    if (!form) return;
    options = options || {};
    var passwordField = options.password ? form.querySelector(options.password) : null;
    var confirmField = options.confirm ? form.querySelector(options.confirm) : null;

    function checkMatch() {
        if (!passwordField || !confirmField) return;
        if (confirmField.value && passwordField.value !== confirmField.value) {
            confirmField.setCustomValidity("mismatch");
        } else {
            confirmField.setCustomValidity("");
        }
    }

    function markField(field, includeEmpty) {
        if (!includeEmpty && field.value === "") {
            field.classList.remove("is-invalid");
            field.classList.remove("is-valid");
            return;
        }
        if (field.checkValidity()) {
            field.classList.add("is-valid");
            field.classList.remove("is-invalid");
        } else {
            field.classList.add("is-invalid");
            field.classList.remove("is-valid");
        }
    }

    var fields = [];
    form.querySelectorAll("input, select, textarea").forEach(function (f) {
        if (f.type !== "hidden") fields.push(f);
    });

    fields.forEach(function (field) {
        field.addEventListener("input", function () {
            if (field === passwordField || field === confirmField) {
                checkMatch();
            }
            markField(field, false);
            if (field === passwordField && confirmField && confirmField.value) {
                markField(confirmField, false);
            }
        });
        field.addEventListener("blur", function () {
            if (field === passwordField || field === confirmField) {
                checkMatch();
            }
            markField(field, false);
        });
    });

    form.addEventListener("submit", function (e) {
        checkMatch();
        fields.forEach(function (field) {
            markField(field, true);
        });
        if (!form.checkValidity()) {
            e.preventDefault();
            e.stopPropagation();
        }
    });

    if (options.modal) {
        options.modal.addEventListener("hidden.bs.modal", function () {
            if (confirmField) confirmField.setCustomValidity("");
            fields.forEach(function (field) {
                field.classList.remove("is-invalid");
                field.classList.remove("is-valid");
            });
        });
    }
}
