<script lang="ts">
    import {page}   from '$app/state';
    import {onMount} from 'svelte';

    let hovered: boolean = false;
    let active : boolean= false;
    let currentActive : '/' | 'chat' = '/';
    
    $: if(typeof page.url.pathname === 'string'){
        currentActive = page.url.pathname as '/' | 'chat';
    }

    



    onMount(() => {
        
    });



</script>