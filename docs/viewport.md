# Viewport

## Qué es el viewport

El viewport es la ventana con la que mirás el canvas. El canvas tiene un tamaño real
fijo; el viewport tiene zoom y desplazamiento. Mover el viewport **no** modifica el
dibujo: es una transformación de cámara.

## Pan (desplazar)

- mantener **Espacio** y arrastrar con el mouse
- botón central del mouse y arrastrar
- con dos dedos en pantalla táctil, arrastrar

El pan no tiene límite: podés desplazarte fuera del área del canvas para trabajar en
los bordes con comodidad.

## Zoom

- rueda del mouse: acerca o aleja en el punto donde está el cursor, no en el centro de
  la pantalla
- pinch con dos dedos en pantalla táctil
- `+` y `-` del teclado: zoom por pasos desde el centro del viewport

El rango de zoom va de 10% a 3200%. El indicador de zoom aparece abajo a la derecha y
se puede hacer clic sobre él para escribir un valor exacto.

## Reset

`Ctrl + 0` restaura el viewport a zoom 100% y centra el canvas en la pantalla. Es el
equivalente a "ajustar a ventana" cuando te perdiste.

## Rotación

El viewport no rota. Si necesitás trabajar en otro ángulo, rotá la imagen importada,
no la cámara.

## Relación con el export

El export usa el tamaño del canvas y la escala elegida, nunca el zoom del viewport.
Puede pasar que veas el dibujo recortado en pantalla y exporte completo, o al revés,
porque el viewport es solo tu cámara.
