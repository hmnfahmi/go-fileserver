(function () {
    function toggleMenu(button) {
        document
            .querySelectorAll(".action-dropdown.show")
            .forEach(function (menu) {
                if (menu !== button.nextElementSibling) {
                    menu.classList.remove("show");
                }
            });

        button
            .nextElementSibling
            .classList
            .toggle("show");
    }

    document.addEventListener("click", function (event) {
        if (!event.target.closest(".action-menu")) {
            document
                .querySelectorAll(".action-dropdown")
                .forEach(function (menu) {
                    menu.classList.remove("show");
                });
        }
    });

    window.toggleMenu = toggleMenu;
})();