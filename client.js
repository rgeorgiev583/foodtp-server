async function loadProducts() {
    function unitsResponseHandler(unitsResponse) {
        unitsResponse.forEach((unit, productUnits) => {
            const unitEntry = document.createElement("option");
            unitEntry.value = unit;
            productUnits.appendChild(unitEntry);
        });
    }

    function productsResponseHandler(productsResponse) {
        productsResponse.forEach(async function product() {
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
            productUnit.list = product + "_units";
            const productUnitTableCell = document.createElement("td");
            productUnitTableCell.appendChild(productUnit);
            productEntry.appendChild(productUnitTableCell);

            const productUnits = document.createElement("datalist");
            productUnits.id = product + "_units";

            const unitsRequestOptions = {
                method: "POST",
                headers: {
                    "Content-Type": "application/json"
                },
                body: JSON.stringify({ "product": product }),
            };
            const response = await fetch("http://foodeta.com:1337/units", unitsRequestOptions);
            const responseObject = await response.json();
            unitsResponseHandler(responseObject, productUnits);

            document.getElementById("products").appendChild(productEntry);
        });
    }

    const productsResponse = await fetch("http://foodeta.com:1337/products");
    const productsResponseObject = await productsResponse.json();
    productsResponseHandler(productsResponseObject);
}

window.onload = loadProducts;