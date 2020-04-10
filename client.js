function loadProducts() {
    function productsResponseHandler() {
        const productsResponse = JSON.parse(this.response);
        productsResponse.forEach(function (product) {
            const productEntry = document.createElement("tr");

            const productCheckbox = document.createElement("input");
            productCheckbox.type = "checkbox";
            productCheckbox.id = product;
            const productNameNode = document.createTextNode(product);
            const productNameTableCell = document.createElement("td");
            productNameTableCell.appendChild(productCheckbox);
            productNameTableCell.appendChild(productNameNode);
            productEntry.appendChild(productNameTableCell);

            const productQuantity = document.createElement("input");
            productQuantity.type = "text";
            productQuantity.id = product + "_quantity";
            const productQuantityTableCell = document.createElement("td");
            productQuantityTableCell.appendChild(productQuantity);
            productEntry.appendChild(productQuantityTableCell);

            const productUnit = document.createElement("input");
            productUnit.type = "text";
            productUnit.id = product + "_unit";
            const productUnitTableCell = document.createElement("td");
            productUnitTableCell.appendChild(productUnit);
            productEntry.appendChild(productUnitTableCell);

            document.getElementById("products").appendChild(productEntry);
        });
    }

    const productsRequest = new XMLHttpRequest();
    productsRequest.addEventListener("load", productsResponseHandler);
    productsRequest.open("GET", "http://localhost:8080/products");
    productsRequest.send();
}

window.onload = loadProducts;