# Proyectos

## Guardar y cargar

Un proyecto se guarda como un único archivo `.json` autocontenido: incluye capas,
trazos, configuración de hervido, imágenes embebidas en base64 y los ajustes de
exportación. No hay archivos auxiliares ni carpeta de recursos.

- **Guardar**: `Ctrl + S` descarga el `.json` con el nombre del proyecto.
- **Abrir**: `Ctrl + O` o arrastrar el `.json` sobre el canvas.

## Autoguardado

El autoguardado corre cada 10 segundos si hubo cambios, y también al cerrar o recargar
la pestaña. Guarda en el almacenamiento local del navegador, no en el sistema de
archivos.

Al abrir la app, si existe un autoguardado más nuevo que el proyecto cargado, aparece
el aviso "hay un autoguardado más reciente" con la opción de recuperarlo o descartarlo.

## Estructura del archivo

```json
{
  "version": 1,
  "name": "mi-proyecto",
  "canvas": { "width": 1920, "height": 1080, "background": "#ffffff" },
  "layers": [
    { "name": "fondo", "visible": true, "locked": false, "strokes": [], "image": null }
  ],
  "settings": { "exportScale": 1, "fps": 12, "duration": 3 }
}
```

## Compatibilidad

El campo `version` identifica el formato. Un archivo de una versión más nueva que la
app se rechaza con el mensaje "el proyecto fue creado con una versión más reciente".
Los archivos sin campo `version` se interpretan como versión 1.

## Límites

- Tamaño máximo del archivo: 50 MB, contando las imágenes embebidas.
- Las imágenes se embeben sin recomprimir, así que un proyecto con varias imágenes
  grandes crece rápido: para compartir, conviene exportar antes que mandar el `.json`.
