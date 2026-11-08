// Renders the API documentation page with the vendored Swagger UI, which is
// loaded before this script. Calls made from the page carry the session
// cookie; a personal API token can be entered under Authorize instead.
(function () {
  var root = document.getElementById("swagger-ui");
  SwaggerUIBundle({
    url: root.dataset.url,
    domNode: root,
    deepLinking: true,
    tryItOutEnabled: false,
  });
})();
