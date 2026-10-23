# Deshacer y rehacer

## Atajos

- **Ctrl + Z**: deshacer
- **Ctrl + Y** o **Ctrl + Shift + Z**: rehacer

## Qué cuenta como un paso

El historial guarda operaciones sobre el documento, no movimientos de cámara. Cada una
de estas acciones es un paso:

- dibujar un trazo completo (un paso, no un paso por punto)
- borrar un trazo con la goma
- crear, renombrar, bloquear, mover o duplicar una capa
- borrar una capa
- importar una imagen, moverla o escalarla (el escalado se agrupa al soltar la manija)
- cambiar los parámetros de hervido de un trazo

Estas acciones **no** son pasos y no se pueden deshacer: pan, zoom, mostrar u ocultar
una capa, cambiar de capa activa y prender o apagar el hervido con la tecla A.

## Límite

El historial guarda los últimos **200 pasos**. Al superar el límite, se descarta el más
antiguo. Cerrar el proyecto o recargar la página vacía el historial: no se guarda en el
archivo `.json` del proyecto.

## Comportamiento al deshacer

Deshacer después de una operación que cambia de capa restaura también la capa activa
que estaba seleccionada en ese momento, para que el contexto visual coincida con el
estado restaurado.

Si deshacés y después hacés una acción nueva, la rama de rehacer se descarta: no hay
historial en árbol, es lineal.
