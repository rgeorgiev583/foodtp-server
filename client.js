function loadProducts() {
    function productsResponseHandler() {
        var productsResponse = JSON.parse(this.response);
        productsResponse.forEach(function (product) {
            var productEntry = document.createElement("tr");

            var productCheckbox = document.createElement("input");
            productCheckbox.type = "checkbox";
            productCheckbox.id = product;
            var productNameNode = document.createTextNode(product);
            var productNameTableCell = document.createElement("td");
            productNameTableCell.appendChild(productCheckbox);
            productNameTableCell.appendChild(productNameNode);
            productEntry.appendChild(productNameTableCell);

            var productQuantity = document.createElement("input");
            productQuantity.type = "text";
            productQuantity.id = product + "_quantity";
            var productQuantityTableCell = document.createElement("td");
            productQuantityTableCell.appendChild(productQuantity);
            productEntry.appendChild(productQuantityTableCell);

            var productUnit = document.createElement("input");
            productUnit.type = "text";
            productUnit.id = product + "_unit";
            var productUnitTableCell = document.createElement("td");
            productUnitTableCell.appendChild(productUnit);
            productEntry.appendChild(productUnitTableCell);

            document.getElementById("products").appendChild(productEntry);
        });
    }

    var productsRequest = new XMLHttpRequest();
    productsRequest.addEventListener("load", productsResponseHandler);
    productsRequest.open("GET", "http://localhost:8080/products");
    productsRequest.send();
}

window.onload = loadProducts;