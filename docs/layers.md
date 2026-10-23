# Capas

## Qué es una capa

Una capa es una pila independiente de trazos e imágenes. El dibujo final es la
composición de arriba hacia abajo: la capa que está más arriba en la lista tapa a las
de abajo.

Cada capa tiene nombre, visibilidad, estado de bloqueo y una posición en el orden.
El límite del proyecto es de 50 capas.

## Operaciones

- **crear**: agrega una capa vacía arriba de la activa y la selecciona.
- **renombrar**: doble clic sobre el nombre en el panel de capas.
- **mostrar/ocultar**: el ícono de ojo. Ocultar no borra nada, solo deja de componer
  esa capa; al exportar, la capa oculta no aparece.
- **bloquear**: el ícono de candado. Una capa bloqueada no acepta trazos, borrados ni
  importación de imágenes, pero se puede seguir viendo.
- **subir/bajar**: mueve la capa un lugar en el orden de composición.
- **duplicar**: copia la capa completa, incluidos trazos y configuración de hervido,
  y la inserta arriba de la original con el sufijo "(copia)".

## Reglas de interacción

- No se puede borrar la última capa: siempre tiene que quedar una.
- Borrar una capa borra sus trazos; la operación se puede deshacer.
- Dibujar sobre una capa bloqueada no hace nada y muestra el aviso "la capa está
  bloqueada".
- Al seleccionar un trazo con la herramienta de selección, la capa activa cambia a la
  capa del trazo.

## Capas e imágenes

Una imagen importada siempre vive en su propia capa nueva, del tipo imagen. No se
puede dibujar encima de una capa de imagen en la misma capa: hay que crear una capa
de dibujo arriba.
